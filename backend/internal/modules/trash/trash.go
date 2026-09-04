package moduletrash

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"homihub/backend/internal/attachments"
	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	modulecalendar "homihub/backend/internal/modules/calendar"
	modulesettings "homihub/backend/internal/modules/settings"
)

// trashItemView is the REST shape of one trashed item shown in the recycle bin.
type trashItemView struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"` // event | todo | note
	Title     string    `json:"title"`
	Calendar  string    `json:"calendar"`
	UserID    string    `json:"userId"`
	DeletedAt time.Time `json:"deletedAt"`
}

// StartTrashCleaner runs the daily 3am purge of expired trashed calendar
// events / todos / notes. File trash has its own cleaner in the files module.
func (h *Handler) StartTrashCleaner() {
	c := cron.New()
	if _, err := c.AddFunc("0 3 * * *", h.purgeExpiredTrash); err == nil {
		c.Start()
		// Run once at startup so long-expired items are cleaned even if the
		// server was down overnight.
		go h.purgeExpiredTrash()
	}
}

// purgeExpiredTrash permanently removes soft-deleted events/notes/todos whose
// deleted_at is older than the configured retention. days == 0 disables it.
func (h *Handler) purgeExpiredTrash() {
	days := modulesettings.TrashItemsRetentionDays(h.app.DB)
	if days <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)

	var evs []models.CalendarEvent
	if err := h.app.DB.Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff).Find(&evs).Error; err == nil {
		for i := range evs {
			ev := evs[i]
			h.purgeEvent(ev.TeamID, ev.ID, ev.UID)
		}
	}
	var todos []models.Todo
	if err := h.app.DB.Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff).Find(&todos).Error; err == nil {
		for i := range todos {
			t := todos[i]
			h.purgeTodo(t.TeamID, t.ID, t.UID)
		}
	}
}

// canManageEvent mirrors the calendar delete permission: owner or parent.
func canManageEvent(cl *middleware.Claims, ev *models.CalendarEvent) bool {
	return ev.UserID == cl.UserID || cl.Role == middleware.RoleParent
}

// canManageTodo mirrors the todos delete permission (canWriteTodo).
func canManageTodo(db *gorm.DB, cl *middleware.Claims, t *models.Todo) bool {
	switch t.Calendar {
	case "self":
		return t.UserID == cl.UserID
	case "team":
		return true
	}
	return modulecalendar.TodoListRole(db, cl.TeamID, cl.UserID, t.Calendar) != ""
}

// canManageNote mirrors the notes delete permission (canWrite).
func canManageNote(db *gorm.DB, cl *middleware.Claims, ev *models.CalendarEvent) bool {
	switch ev.Calendar {
	case "self":
		return ev.UserID == cl.UserID
	case "team":
		return true
	}
	return modulecalendar.TodoListRole(db, cl.TeamID, cl.UserID, ev.Calendar) != ""
}

// listTrash returns the soft-deleted items the caller may manage, optionally
// filtered by kind (event | todo | note).
func (h *Handler) listTrash(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	kind := c.Query("kind")
	sc := func() *gorm.DB { return middleware.ScopedDB(h.app.DB, cl.TeamID).Unscoped() }
	out := []trashItemView{}

	if kind == "" || kind == "event" || kind == "note" {
		q := sc().Where("deleted_at IS NOT NULL")
		if kind == "note" {
			q = q.Where("component_type = ?", "VJOURNAL")
		} else if kind == "event" {
			q = q.Where("component_type <> ?", "VJOURNAL")
		}
		var evs []models.CalendarEvent
		if err := q.Order("deleted_at DESC").Find(&evs).Error; err == nil {
			for i := range evs {
				ev := &evs[i]
				if kind == "note" {
					if !canManageNote(sc(), cl, ev) {
						continue
					}
				} else if !canManageEvent(cl, ev) {
					continue
				}
				out = append(out, trashItemView{
					ID: ev.ID, Kind: "event", Title: ev.Title,
					Calendar: ev.Calendar, UserID: ev.UserID, DeletedAt: ev.DeletedAt.Time,
				})
			}
		}
	}
	if kind == "" || kind == "todo" {
		q := sc().Where("deleted_at IS NOT NULL")
		var todos []models.Todo
		if err := q.Order("deleted_at DESC").Find(&todos).Error; err == nil {
			for i := range todos {
				t := &todos[i]
				if !canManageTodo(sc(), cl, t) {
					continue
				}
				out = append(out, trashItemView{
					ID: t.ID, Kind: "todo", Title: t.Title,
					Calendar: t.Calendar, UserID: t.UserID, DeletedAt: t.DeletedAt.Time,
				})
			}
		}
	}
	httpx.OK(c, out)
}

// restoreTrash clears deleted_at on the item (and its invitee copies) and logs
// the CalDAV resurrection so external clients see the object come back.
func (h *Handler) restoreTrash(c *gin.Context) {
	kind := c.Param("kind")
	id := c.Param("id")
	cl := middleware.ClaimsOf(c)
	sc := func() *gorm.DB { return middleware.ScopedDB(h.app.DB, cl.TeamID) }

	switch kind {
	case "event", "note":
		var ev models.CalendarEvent
		if err := sc().Unscoped().Where("id = ? AND deleted_at IS NOT NULL", id).First(&ev).Error; err != nil {
			httpx.NotFoundT(c, "item_not_found")
			return
		}
		if kind == "note" && !canManageNote(sc(), cl, &ev) ||
			kind == "event" && !canManageEvent(cl, &ev) {
			httpx.ForbiddenT(c, "forbidden")
			return
		}
		if err := sc().Unscoped().Model(&models.CalendarEvent{}).Where("id = ?", ev.ID).
			Updates(map[string]any{"deleted_at": nil}).Error; err != nil {
			httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
			return
		}
		modulecalendar.RestoreEventInviteeCopies(sc(), ev.UID)
		modulecalendar.LogCalendarObjectSync(h.app.DB, cl.TeamID, &ev, false)
	case "todo":
		var t models.Todo
		if err := sc().Unscoped().Where("id = ? AND deleted_at IS NOT NULL", id).First(&t).Error; err != nil {
			httpx.NotFoundT(c, "item_not_found")
			return
		}
		if !canManageTodo(sc(), cl, &t) {
			httpx.ForbiddenT(c, "forbidden")
			return
		}
		if err := sc().Unscoped().Model(&models.Todo{}).Where("id = ?", t.ID).
			UpdateColumn("deleted_at", nil).Error; err != nil {
			httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
			return
		}
		modulecalendar.RestoreTodoInviteeCopies(sc(), t.UID)
		modulecalendar.LogTodoObjectSync(h.app.DB, cl.TeamID, &t, false)
	default:
		httpx.BadRequestT(c, "bad_request")
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}

// purgeTrash permanently removes a trashed item: its attachments, any invitee
// copies sharing the UID, and the row itself.
func (h *Handler) purgeTrash(c *gin.Context) {
	kind := c.Param("kind")
	id := c.Param("id")
	cl := middleware.ClaimsOf(c)
	sc := func() *gorm.DB { return middleware.ScopedDB(h.app.DB, cl.TeamID) }

	switch kind {
	case "event", "note":
		var ev models.CalendarEvent
		if err := sc().Unscoped().Where("id = ? AND deleted_at IS NOT NULL", id).First(&ev).Error; err != nil {
			httpx.NotFoundT(c, "item_not_found")
			return
		}
		if kind == "note" && !canManageNote(sc(), cl, &ev) ||
			kind == "event" && !canManageEvent(cl, &ev) {
			httpx.ForbiddenT(c, "forbidden")
			return
		}
		h.purgeEvent(ev.TeamID, ev.ID, ev.UID)
	case "todo":
		var t models.Todo
		if err := sc().Unscoped().Where("id = ? AND deleted_at IS NOT NULL", id).First(&t).Error; err != nil {
			httpx.NotFoundT(c, "item_not_found")
			return
		}
		if !canManageTodo(sc(), cl, &t) {
			httpx.ForbiddenT(c, "forbidden")
			return
		}
		h.purgeTodo(t.TeamID, t.ID, t.UID)
	default:
		httpx.BadRequestT(c, "bad_request")
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}

func (h *Handler) purgeEvent(teamID, id, uid string) {
	sc := func() *gorm.DB { return middleware.ScopedDB(h.app.DB, teamID) }
	_ = attachments.DeleteForItem(h.app.DB, teamID, id)
	modulecalendar.PurgeEventInviteeCopies(sc(), uid)
	_ = sc().Unscoped().Where("id = ?", id).Delete(&models.CalendarEvent{}).Error
}

func (h *Handler) purgeTodo(teamID, id, uid string) {
	sc := func() *gorm.DB { return middleware.ScopedDB(h.app.DB, teamID) }
	_ = attachments.DeleteForItem(h.app.DB, teamID, id)
	modulecalendar.PurgeTodoInviteeCopies(sc(), uid)
	_ = sc().Unscoped().Where("id = ?", id).Delete(&models.Todo{}).Error
}
