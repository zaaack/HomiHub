package modulefiles

import (
	"net/http"
	"time"

	"github.com/emersion/go-ical"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"homihub/backend/internal/attachments"
	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	modulecalendar "homihub/backend/internal/modules/calendar"
)

// attachmentItemAccess resolves the target item (event | todo | note) and
// reports whether the caller may view / write it. The rules mirror each item's
// own REST permissions so attachments never widen access.
func attachmentItemAccess(db *gorm.DB, cl *middleware.Claims, kind, itemID string) (visible, writable bool) {
	switch kind {
	case "event", "note":
		var ev models.CalendarEvent
		if err := db.First(&ev, "id = ?", itemID).Error; err != nil {
			return false, false
		}
		if kind == "note" && ev.ComponentType != ical.CompJournal {
			return false, false
		}
		if kind == "event" && ev.ComponentType == ical.CompJournal {
			return false, false
		}
		writable = ev.UserID == cl.UserID || cl.Role == middleware.RoleParent
		visible = writable || ev.Visibility != models.VisibilityPrivate
		return visible, writable
	case "todo":
		var td models.Todo
		if err := db.First(&td, "id = ?", itemID).Error; err != nil {
			return false, false
		}
		switch td.Calendar {
		case "self":
			return td.UserID == cl.UserID, td.UserID == cl.UserID
		case "team":
			return true, true
		default:
			role := modulecalendar.TodoListRole(db, cl.TeamID, cl.UserID, td.Calendar)
			return role != "", role != ""
		}
	}
	return false, false
}

// attachmentScope derives the storage scope (team/public vs personal) from the
// item's calendar so personal items keep their files personal.
func attachmentScope(db *gorm.DB, kind, itemID string) string {
	switch kind {
	case "event", "note":
		var ev models.CalendarEvent
		if err := db.First(&ev, "id = ?", itemID).Error; err == nil && ev.Calendar == "self" {
			return models.ScopePersonal
		}
	case "todo":
		var td models.Todo
		if err := db.First(&td, "id = ?", itemID).Error; err == nil && td.Calendar == "self" {
			return models.ScopePersonal
		}
	}
	return models.ScopePublic
}

func (h *Handler) createAttachment(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	kind := c.PostForm("kind")
	itemID := c.PostForm("itemId")
	if kind != "event" && kind != "todo" && kind != "note" {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	_, writable := attachmentItemAccess(middleware.DB(c), cl, kind, itemID)
	if !writable {
		httpx.NotFoundT(c, "item_not_found")
		return
	}
	scope := attachmentScope(middleware.DB(c), kind, itemID)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	fileID := uuid.Must(uuid.NewV7()).String()
	key := contentKey(cl.TeamID, scope, fileID)
	part, err := fileHeader.Open()
	if err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	defer part.Close()
	if err := h.st.Save(c.Request.Context(), key, part); err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	f := models.File{
		ID:           fileID,
		TeamID:       cl.TeamID,
		Scope:        scope,
		OwnerID:      cl.UserID,
		Name:         fileHeader.Filename,
		MimeType:     fileHeader.Header.Get("Content-Type"),
		Size:         fileHeader.Size,
		AttachmentOf: itemID,
	}
	if err := middleware.DB(c).Create(&f).Error; err != nil {
		h.st.Delete(c.Request.Context(), key)
		httpx.ErrT(c, http.StatusInternalServerError, "create_failed")
		return
	}
	att := models.Attachment{
		ID:     uuid.Must(uuid.NewV7()).String(),
		TeamID: cl.TeamID,
		Kind:   kind,
		ItemID: itemID,
		FileID: fileID,
		UserID: cl.UserID,
		Scope:  scope,
	}
	if err := middleware.DB(c).Create(&att).Error; err != nil {
		now := time.Now()
		middleware.DB(c).Model(&models.File{}).Where("id = ?", fileID).Update("deleted_at", &now)
		h.st.Delete(c.Request.Context(), key)
		httpx.ErrT(c, http.StatusInternalServerError, "create_failed")
		return
	}
	httpx.Created(c, models.AttachmentView{
		Attachment: att,
		Name:       f.Name,
		MimeType:   f.MimeType,
		Size:       f.Size,
		URL:        "/api/v1/files/" + f.ID + "/content",
	})
}

func (h *Handler) listAttachments(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	kind := c.Query("kind")
	itemID := c.Query("itemId")
	if kind != "event" && kind != "todo" && kind != "note" {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	visible, _ := attachmentItemAccess(middleware.DB(c), cl, kind, itemID)
	if !visible {
		httpx.NotFoundT(c, "item_not_found")
		return
	}
	httpx.OK(c, attachments.ViewsForItem(h.app.DB, cl.TeamID, kind, itemID))
}

// listManage lists every attachment the caller may view, annotated with the
// owning item so the UI can show which event / note / todo it belongs to
// (reverse lookup). Filters: kind (event|note|todo), completed (true|false)
// and a created_at range (from/to, RFC3339).
func (h *Handler) listManage(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	kind := c.Query("kind")
	if kind != "" && kind != "event" && kind != "note" && kind != "todo" {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	sc := func() *gorm.DB { return middleware.ScopedDB(h.app.DB, cl.TeamID) }
	q := sc().Model(&models.Attachment{})
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	if from := c.Query("from"); from != "" {
		q = q.Where("created_at >= ?", from)
	}
	if to := c.Query("to"); to != "" {
		q = q.Where("created_at <= ?", to)
	}
	var atts []models.Attachment
	if err := q.Order("created_at desc").Find(&atts).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "query_failed")
		return
	}
	if len(atts) == 0 {
		httpx.OK(c, []models.AttachmentManageView{})
		return
	}
	completedFilter := ""
	if v := c.Query("completed"); v == "true" || v == "false" {
		completedFilter = v
	}
	now := time.Now()
	out := make([]models.AttachmentManageView, 0, len(atts))
	for _, a := range atts {
		item := attachmentManageItem(middleware.DB(c), cl, a, now)
		if item == nil {
			continue // item gone or caller cannot see it
		}
		if completedFilter == "true" && !item.Completed {
			continue
		}
		if completedFilter == "false" && item.Completed {
			continue
		}
		out = append(out, models.AttachmentManageView{
			AttachmentView: attachmentView(h.app.DB, cl.TeamID, a),
			Item:           item,
		})
	}
	httpx.OK(c, out)
}

// attachmentManageItem resolves an attachment's owning item. It returns nil
// when the item no longer exists or the caller may not view it. Visibility
// reuses attachmentItemAccess so the manage list never widens access.
func attachmentManageItem(db *gorm.DB, cl *middleware.Claims, a models.Attachment, now time.Time) *models.AttachmentItemInfo {
	visible, _ := attachmentItemAccess(db, cl, a.Kind, a.ItemID)
	if !visible {
		return nil
	}
	switch a.Kind {
	case "event", "note":
		var ev models.CalendarEvent
		if err := db.First(&ev, "id = ?", a.ItemID).Error; err != nil {
			return nil
		}
		st, en := ev.StartsAt, ev.EndsAt
		return &models.AttachmentItemInfo{
			ID:        ev.ID,
			Kind:      a.Kind,
			Title:     ev.Title,
			Calendar:  ev.Calendar,
			Completed: !en.IsZero() && en.Before(now),
			StartsAt:  &st,
			EndsAt:    &en,
		}
	case "todo":
		var td models.Todo
		if err := db.First(&td, "id = ?", a.ItemID).Error; err != nil {
			return nil
		}
		due := td.DueAt
		return &models.AttachmentItemInfo{
			ID:        td.ID,
			Kind:      a.Kind,
			Title:     td.Title,
			Calendar:  td.Calendar,
			Completed: td.Completed,
			DueAt:     due,
		}
	}
	return nil
}

// attachmentView builds the file-backed view (name/type/size/url) for an
// attachment, or an empty view if its file is gone.
func attachmentView(base *gorm.DB, teamID string, a models.Attachment) models.AttachmentView {
	v := models.AttachmentView{Attachment: a}
	var f models.File
	sc := func() *gorm.DB { return middleware.ScopedDB(base, teamID) }
	if err := sc().First(&f, "id = ?", a.FileID).Error; err != nil {
		return v
	}
	v.Name = f.Name
	v.MimeType = f.MimeType
	v.Size = f.Size
	v.URL = "/api/v1/files/" + f.ID + "/content"
	return v
}

func (h *Handler) deleteAttachment(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var att models.Attachment
	if err := middleware.DB(c).First(&att, "id = ?", c.Param("id")).Error; err != nil {
		httpx.NotFoundT(c, "attachment_not_found")
		return
	}
	// Only the uploader (or an admin) may delete an attachment; item access
	// alone is not enough.
	if att.UserID != cl.UserID && cl.Role != middleware.RoleParent {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	if err := attachments.DeleteOne(h.app.DB, cl.TeamID, att); err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "delete_failed")
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}
