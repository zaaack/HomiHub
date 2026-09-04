package moduletodos

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"homihub/backend/internal/attachments"
	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	modulecalendar "homihub/backend/internal/modules/calendar"
)

// Defaults for todo lists created from the web UI (a list is a VTODO-only
// calendar row so tasks.org / DAVx5 sync it as a task list).
const (
	defaultListColor = "#4f8cff"
	defaultListIcon  = "📋"
)

type memberBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type todoListView struct {
	ID       string        `json:"id"` // calendar name: "self", "team", or a custom slug
	Kind     string        `json:"kind"`
	Name     string        `json:"name"` // display name (built-ins are localized by the web UI)
	Color    string        `json:"color"`
	Icon     string        `json:"icon"`
	OwnerID  string        `json:"ownerId"`
	Access   string        `json:"access"`
	CanEdit  bool          `json:"canEdit"`
	Writable bool          `json:"writable"`
	Members  []memberBrief `json:"members"`
}

type todoListInput struct {
	Name      string   `json:"name"`
	Color     string   `json:"color"`
	Icon      string   `json:"icon"`
	MemberIDs []string `json:"memberIds"`
}

// normalizeListInput trims/validates a list payload; false means invalid.
func normalizeListInput(in *todoListInput) bool {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 64 {
		return false
	}
	in.Color = normalizeColor(in.Color)
	return len([]rune(in.Icon)) <= 8
}

// normalizeColor coerces #rrggbb (or rrggbb) to canonical #rrggbb.
func normalizeColor(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(s, "#"))
	if len(s) != 6 {
		return defaultListColor
	}
	for _, ch := range s {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F') {
			return defaultListColor
		}
	}
	return "#" + strings.ToLower(s)
}

// scoped returns a fresh team-scoped handle for one operation (GORM reuses the
// underlying statement otherwise, so every chain gets its own handle).
func (h *Handler) scoped(teamID string) *gorm.DB {
	return middleware.ScopedDB(h.app.DB, teamID)
}

// resolveTeamMemberIDs keeps only ids that belong to the current team and drops
// the caller itself (the owner always keeps access).
func resolveTeamMemberIDs(db *gorm.DB, teamID, ownerID string, ids []string) []string {
	seen := map[string]bool{ownerID: true}
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return unique
	}
	var rows []models.TeamMember
	if err := db.Where("user_id IN ?", unique).Find(&rows).Error; err != nil {
		return []string{}
	}
	members := make([]string, 0, len(rows))
	for _, r := range rows {
		if seen[r.UserID] {
			members = append(members, r.UserID)
		}
	}
	return members
}

func setShares(h *Handler, teamID, calName string, memberIDs []string) error {
	base := h.scoped(teamID)
	if err := base.Where("calendar = ?", calName).Delete(&models.CalendarShare{}).Error; err != nil {
		return err
	}
	for _, uid := range memberIDs {
		share := models.CalendarShare{TeamID: teamID, Calendar: calName, UserID: uid}
		if err := h.scoped(teamID).Create(&share).Error; err != nil {
			return err
		}
	}
	return nil
}

// loadListMembers resolves the sharee rows of a list into brief member views.
// A fresh scoped handle is used so the caller's statement is never reused.
func (h *Handler) loadListMembers(teamID, calName string) []memberBrief {
	var shares []models.CalendarShare
	if err := h.scoped(teamID).Where("calendar = ?", calName).Find(&shares).Error; err != nil || len(shares) == 0 {
		return nil
	}
	ids := make([]string, 0, len(shares))
	for _, s := range shares {
		ids = append(ids, s.UserID)
	}
	var users []models.User
	if err := h.scoped(teamID).Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil
	}
	byID := map[string]string{}
	for _, u := range users {
		byID[u.ID] = u.Name
	}
	out := make([]memberBrief, 0, len(shares))
	for _, s := range shares {
		out = append(out, memberBrief{ID: s.UserID, Name: byID[s.UserID]})
	}
	return out
}

func (h *Handler) toTodoListView(c *models.Calendar, ownerID string) todoListView {
	v := todoListView{
		ID:       c.Name,
		Kind:     "custom",
		Name:     c.DisplayName,
		Color:    c.Color,
		Icon:     c.Icon,
		OwnerID:  c.OwnerID,
		Access:   c.Access,
		Writable: true,
	}
	if c.Access == models.CalendarAccessMembers {
		members := h.loadListMembers(c.TeamID, c.Name)
		v.Members = members
		v.CanEdit = c.OwnerID == ownerID
		v.Writable = c.OwnerID == ownerID
		for _, m := range members {
			if m.ID == ownerID {
				v.Writable = true
			}
		}
		if v.Color == "" {
			v.Color = defaultListColor
		}
	}
	return v
}

// listTodoLists returns the todo lists the caller may use: the built-in
// personal ("self") and team ("team") lists plus every accessible custom
// VTODO calendar.
func (h *Handler) listTodoLists(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	base := []todoListView{
		{ID: "self", Kind: "personal", Access: models.CalendarAccessLegacy, Writable: true},
		{ID: "team", Kind: "team", Access: models.CalendarAccessLegacy, Writable: true},
	}
	cals := modulecalendar.TodoListsForUser(h.scoped(cl.TeamID), cl.TeamID, cl.UserID)
	out := make([]todoListView, 0, len(base)+len(cals))
	out = append(out, base...)
	for i := range cals {
		out = append(out, h.toTodoListView(&cals[i], cl.UserID))
	}
	httpx.OK(c, out)
}

func (h *Handler) createTodoList(c *gin.Context) {
	var in todoListInput
	if !httpx.Bind(c, &in) || !normalizeListInput(&in) {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	cl := middleware.ClaimsOf(c)
	slug := "list-" + strings.ReplaceAll(uuid.Must(uuid.NewV7()).String(), "-", "")
	cal := models.Calendar{
		ID:          uuid.Must(uuid.NewV7()).String(),
		TeamID:      cl.TeamID,
		Name:        slug,
		DisplayName: in.Name,
		Color:       normalizeColor(in.Color),
		Icon:        in.Icon,
		Components:  "VTODO",
		Access:      models.CalendarAccessMembers,
		OwnerID:     cl.UserID,
	}
	if cal.Icon == "" {
		cal.Icon = defaultListIcon
	}
	db := h.scoped(cl.TeamID)
	members := resolveTeamMemberIDs(db, cl.TeamID, cl.UserID, in.MemberIDs)
	if err := h.scoped(cl.TeamID).Create(&cal).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "create_failed")
		return
	}
	if err := setShares(h, cl.TeamID, slug, members); err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "create_failed")
		return
	}
	httpx.Created(c, h.toTodoListView(&cal, cl.UserID))
}

// updateTodoList renames/recolors a member-scoped list and replaces the member
// set. Only the owner may edit; built-in lists are not editable here.
func (h *Handler) updateTodoList(c *gin.Context) {
	var in todoListInput
	if !httpx.Bind(c, &in) || !normalizeListInput(&in) {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	cl := middleware.ClaimsOf(c)
	id := c.Param("id")
	var cal models.Calendar
	if err := h.scoped(cl.TeamID).
		Where("name = ? AND access = ?", id, models.CalendarAccessMembers).First(&cal).Error; err != nil {
		httpx.NotFoundT(c, "todo_list_not_found")
		return
	}
	if cal.OwnerID != cl.UserID {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	if err := h.scoped(cl.TeamID).Model(&models.Calendar{}).Where("id = ?", cal.ID).Updates(map[string]any{
		"display_name": in.Name,
		"color":        normalizeColor(in.Color),
		"icon":         in.Icon,
	}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	if err := setShares(h, cl.TeamID, id, resolveTeamMemberIDs(h.scoped(cl.TeamID), cl.TeamID, cl.UserID, in.MemberIDs)); err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	cal.DisplayName = in.Name
	cal.Color = normalizeColor(in.Color)
	cal.Icon = in.Icon
	httpx.OK(c, h.toTodoListView(&cal, cl.UserID))
}

// deleteTodoList removes a member-scoped list together with its calendar row,
// share relations and every todo in it. Only the owner may delete.
func (h *Handler) deleteTodoList(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	id := c.Param("id")
	var cal models.Calendar
	if err := h.scoped(cl.TeamID).
		Where("name = ? AND access = ?", id, models.CalendarAccessMembers).First(&cal).Error; err != nil {
		httpx.NotFoundT(c, "todo_list_not_found")
		return
	}
	if cal.OwnerID != cl.UserID {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	if err := h.scoped(cl.TeamID).Where("calendar = ?", id).Delete(&models.CalendarShare{}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "delete_failed")
		return
	}
	var todos []models.Todo
	if err := h.scoped(cl.TeamID).Where("calendar = ?", id).Find(&todos).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "query_failed")
		return
	}
	for i := range todos {
		recordTodoLog(h.scoped(cl.TeamID), cl.TeamID, cl.UserID, todos[i].ID, "delete", "")
		_ = attachments.DeleteForItem(h.app.DB, cl.TeamID, todos[i].ID)
	}
	if err := h.scoped(cl.TeamID).Unscoped().Where("calendar = ?", id).Delete(&models.Todo{}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "delete_failed")
		return
	}
	if err := h.scoped(cl.TeamID).Where("id = ?", cal.ID).Delete(&models.Calendar{}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "delete_failed")
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}
