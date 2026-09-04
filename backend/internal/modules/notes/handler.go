package modulenotes

import (
	"net/http"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	"homihub/backend/internal/modules"
	modulecalendar "homihub/backend/internal/modules/calendar"
)

// Handler serves the notes module. A note is a VJOURNAL calendar object stored
// as a models.CalendarEvent row (ComponentType = VJOURNAL) inside a calendar:
// personal notes live in "self", team notes in "team", and member-shared notes
// in note lists (custom calendars whose Components = "VJOURNAL"). This keeps
// notes fully round-trippable through CalDAV while the REST layer below is what
// the web UI talks to.
type Handler struct {
	app *modules.App
}

func (h *Handler) ID() string { return "notes" }

func (h *Handler) Init(app *modules.App) error {
	h.app = app
	return nil
}

func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	auth := middleware.Auth(h.app.DB, h.app.Config.JWTSecret)
	g.GET("/notes", auth, h.list)
	g.POST("/notes", auth, middleware.RequireWriteRole(), h.create)
	g.PUT("/notes/:id", auth, middleware.RequireWriteRole(), h.update)
	g.DELETE("/notes/:id", auth, middleware.RequireWriteRole(), h.delete)
	g.GET("/note-lists", auth, h.listLists)
	g.POST("/note-lists", auth, middleware.RequireWriteRole(), h.createList)
	g.PUT("/note-lists/:id", auth, middleware.RequireWriteRole(), h.updateList)
	g.DELETE("/note-lists/:id", auth, middleware.RequireWriteRole(), h.deleteList)
}

// scoped returns a fresh team-scoped handle for one operation (GORM reuses the
// underlying statement otherwise, so every chain gets its own handle).
func (h *Handler) scoped(teamID string) *gorm.DB {
	return middleware.ScopedDB(h.app.DB, teamID)
}

// noteView is the REST shape of a note.
type noteView struct {
	ID        string             `json:"id"`
	UID       string             `json:"uid"`
	TeamID    string             `json:"teamId"`
	UserID    string             `json:"userId"`
	Calendar  string             `json:"calendar"`
	Title     string             `json:"title"`
	Body      string             `json:"body"`
	Tags      string             `json:"tags"`
	Attendees []models.Attendee  `json:"attendees"`
	CreatedAt time.Time          `json:"createdAt"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

func toNoteView(ev *models.CalendarEvent) noteView {
	return noteView{
		ID:        ev.ID,
		UID:       ev.UID,
		TeamID:    ev.TeamID,
		UserID:    ev.UserID,
		Calendar:  ev.Calendar,
		Title:     ev.Title,
		Body:      ev.Description,
		Tags:      ev.Tags,
		Attendees: models.ParseAttendees(ev.Attendees),
		CreatedAt: ev.CreatedAt,
		UpdatedAt: ev.UpdatedAt,
	}
}

type noteInput struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Tags      string   `json:"tags"`
	Calendar  string   `json:"calendar"` // "self", "team", or a custom note-list calendar
	Attendees []string `json:"attendees"` // team member ids to invite (VJOURNAL ATTENDEE)
}

func normalizeNote(in *noteInput) bool {
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(in.Body)
	in.Tags = strings.Trim(strings.TrimSpace(in.Tags), ",")
	if in.Title == "" && in.Body == "" {
		return false // an empty note is not saved
	}
	if len([]rune(in.Tags)) > 500 {
		return false
	}
	if in.Calendar == "" {
		in.Calendar = "self"
	}
	return true
}

// noteCalNames returns the custom calendars (excluding built-in self/team)
// whose VJOURNAL notes the caller may read.
func (h *Handler) noteCalNames(db *gorm.DB, teamID, userID string) []string {
	cals := modulecalendar.NotesListsForUser(db, teamID, userID)
	names := make([]string, 0, len(cals))
	for i := range cals {
		names = append(names, cals[i].Name)
	}
	return names
}

// list returns the notes the caller may read: their own personal notes, the
// team notes, and the notes of every accessible note list.
func (h *Handler) list(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	custom := h.noteCalNames(h.scoped(cl.TeamID), cl.TeamID, cl.UserID)
	parts := []string{"(calendar = ? AND user_id = ?) OR calendar = ?"}
	args := []any{"self", cl.UserID, "team"}
	if len(custom) > 0 {
		parts = append(parts, "calendar IN ?")
		args = append(args, custom)
	}
	var notes []models.CalendarEvent
	if err := h.scoped(cl.TeamID).Where(strings.Join(parts, " OR "), args...).
		Where("component_type = ?", ical.CompJournal).
		Order("updated_at DESC").Find(&notes).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "query_failed")
		return
	}
	out := make([]noteView, 0, len(notes))
	for i := range notes {
		out = append(out, toNoteView(&notes[i]))
	}
	httpx.OK(c, out)
}

// writableNoteCal reports whether notes may be created in / moved to a
// calendar: built-in self/team or an accessible custom note list.
func (h *Handler) writableNoteCal(cl *middleware.Claims, calName string) bool {
	if calName == "team" || calName == "self" {
		return true
	}
	return modulecalendar.TodoListRole(h.scoped(cl.TeamID), cl.TeamID, cl.UserID, calName) != ""
}

func (h *Handler) create(c *gin.Context) {
	var in noteInput
	if !httpx.Bind(c, &in) || !normalizeNote(&in) {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	cl := middleware.ClaimsOf(c)
	if !h.writableNoteCal(cl, in.Calendar) {
		httpx.NotFoundT(c, "todo_list_not_found")
		return
	}
	vis := models.VisibilityTeam
	if in.Calendar == "self" {
		vis = models.VisibilityPrivate
	}
	ev := models.CalendarEvent{
		ID:            uuid.Must(uuid.NewV7()).String(),
		TeamID:        cl.TeamID,
		UserID:        cl.UserID,
		UID:           "journal-" + uuid.Must(uuid.NewV7()).String(),
		Calendar:      in.Calendar,
		ComponentType: ical.CompJournal,
		Title:         in.Title,
		Description:   in.Body,
		Tags:          in.Tags,
		Visibility:    vis,
	}
	// Resolve invited members before writing so the row stores the resolved set.
	attendees, users := modulecalendar.ResolveInvitees(h.app.DB, cl.TeamID, in.Attendees)
	ev.Attendees = models.AttendeesJSON(attendees)
	if err := h.scoped(cl.TeamID).Create(&ev).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "create_failed")
		return
	}
	// Invitees that cannot already see the list get a private copy in their own
	// personal (self) space, mirroring the event/todo invite flow.
	modulecalendar.SyncEventInvites(h.app.DB, cl.TeamID, &ev, users, nil, cl.UserID)
	modulecalendar.LogCalendarObjectSync(h.app.DB, cl.TeamID, &ev, false)
	httpx.Created(c, toNoteView(&ev))
}

func (h *Handler) canWrite(cl *middleware.Claims, ev *models.CalendarEvent) bool {
	switch ev.Calendar {
	case "self":
		return ev.UserID == cl.UserID
	case "team":
		return true
	}
	return modulecalendar.TodoListRole(h.scoped(cl.TeamID), cl.TeamID, cl.UserID, ev.Calendar) != ""
}

func (h *Handler) update(c *gin.Context) {
	var in noteInput
	if !httpx.Bind(c, &in) || !normalizeNote(&in) {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	id := c.Param("id")
	cl := middleware.ClaimsOf(c)
	var existing models.CalendarEvent
	if err := h.scoped(cl.TeamID).
		Where("id = ? AND component_type = ?", id, ical.CompJournal).First(&existing).Error; err != nil {
		httpx.NotFoundT(c, "note_not_found")
		return
	}
	if !h.canWrite(cl, &existing) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	updates := map[string]any{
		"title":       in.Title,
		"description": in.Body,
		"tags":        in.Tags,
		"updated_at":  time.Now().UTC(),
	}
	// Moving a note to another accessible list (or back to personal).
	if in.Calendar != "" && in.Calendar != existing.Calendar {
		if in.Calendar == "self" && existing.UserID != cl.UserID {
			httpx.ForbiddenT(c, "forbidden")
			return
		}
		if !h.writableNoteCal(cl, in.Calendar) {
			httpx.NotFoundT(c, "todo_list_not_found")
			return
		}
		updates["calendar"] = in.Calendar
		if in.Calendar == "self" {
			updates["visibility"] = models.VisibilityPrivate
		} else {
			updates["visibility"] = models.VisibilityTeam
		}
	}
	// Re-invite members: resolve the new set and write it before syncing copies.
	prev := models.ParseAttendees(existing.Attendees)
	attendees, users := modulecalendar.ResolveInvitees(h.app.DB, cl.TeamID, in.Attendees)
	updates["attendees"] = models.AttendeesJSON(attendees)
	if err := h.scoped(cl.TeamID).Model(&models.CalendarEvent{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	var ev models.CalendarEvent
	h.scoped(cl.TeamID).Where("id = ?", id).First(&ev)
	modulecalendar.SyncEventInvites(h.app.DB, cl.TeamID, &ev, users, prev, cl.UserID)
	modulecalendar.LogCalendarObjectSync(h.app.DB, cl.TeamID, &ev, false)
	httpx.OK(c, toNoteView(&ev))
}

func (h *Handler) delete(c *gin.Context) {
	id := c.Param("id")
	cl := middleware.ClaimsOf(c)
	var ev models.CalendarEvent
	if err := h.scoped(cl.TeamID).
		Where("id = ? AND component_type = ?", id, ical.CompJournal).First(&ev).Error; err != nil {
		httpx.NotFoundT(c, "note_not_found")
		return
	}
	if !h.canWrite(cl, &ev) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	if err := h.scoped(cl.TeamID).Where("id = ?", id).Delete(&models.CalendarEvent{}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "delete_failed")
		return
	}
	// Remove invitee personal copies (same UID) of a deleted note.
	inviteeIDs := make([]string, 0, 4)
	for _, a := range models.ParseAttendees(ev.Attendees) {
		if a.ID != "" {
			inviteeIDs = append(inviteeIDs, a.ID)
		}
	}
	modulecalendar.SoftDeleteEventInviteeCopies(h.scoped(ev.TeamID), ev.UID, inviteeIDs)
	modulecalendar.LogCalendarObjectSync(h.app.DB, cl.TeamID, &ev, true)
	httpx.OK(c, gin.H{"ok": true})
}
