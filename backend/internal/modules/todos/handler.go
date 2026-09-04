package moduletodos

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/teambition/rrule-go"
	"gorm.io/gorm"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	"homihub/backend/internal/modules"
	modulecalendar "homihub/backend/internal/modules/calendar"
)

// recordTodoLog appends a mutation to the team's todo audit trail.
func recordTodoLog(db *gorm.DB, teamID, userID, todoID, action, details string) {
	_ = db.Create(&models.TodoLog{
		TeamID:  teamID,
		TodoID:  todoID,
		UserID:  userID,
		Action:  action,
		Details: details,
	}).Error
}

type Handler struct {
	app *modules.App
}

func (h *Handler) ID() string { return "todos" }

func (h *Handler) Init(app *modules.App) error {
	h.app = app
	return nil
}

func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	auth := middleware.Auth(h.app.DB, h.app.Config.JWTSecret)
	g.GET("/todos", auth, h.list)
	g.POST("/todos", auth, middleware.RequireWriteRole(), h.create)
	g.PUT("/todos/:id", auth, middleware.RequireWriteRole(), h.update)
	g.PATCH("/todos/:id/toggle", auth, middleware.RequireWriteRole(), h.toggle)
	g.DELETE("/todos/:id", auth, middleware.RequireWriteRole(), h.delete)
	g.GET("/todo-lists", auth, h.listTodoLists)
	g.POST("/todo-lists", auth, middleware.RequireWriteRole(), h.createTodoList)
	g.PUT("/todo-lists/:id", auth, middleware.RequireWriteRole(), h.updateTodoList)
	g.DELETE("/todo-lists/:id", auth, middleware.RequireWriteRole(), h.deleteTodoList)
}

type todoView struct {
	models.Todo
	HasDate   bool              `json:"hasDate"`
	Reminders []models.Reminder `json:"reminders"`
	ExDates   []string          `json:"exdates"`
	Attendees []models.Attendee `json:"attendees"`
}

func toTodoView(t models.Todo) todoView {
	return todoView{
		Todo:      t,
		HasDate:   t.DueAt != nil,
		Reminders: models.ParseReminders(t.Reminders),
		ExDates:   exDatesToStrs(models.ExDateSplit(t.ExDate)),
		Attendees: models.ParseAttendees(t.Attendees),
	}
}

// exDatesToStrs renders UTC times as RFC3339 strings.
func exDatesToStrs(ds []time.Time) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.UTC().Format(time.RFC3339))
	}
	return out
}

// list returns the current user's personal todos plus team-shared todos and
// the todos of every custom list they can access.
func (h *Handler) list(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	custom := accessibleTodoCalNames(middleware.DB(c), cl)
	// Keep every branch inside one WHERE group so the tenant scope (ANDed by
	// middleware) applies to the whole OR chain.
	parts := []string{"(calendar = ? AND user_id = ?) OR calendar = ?"}
	args := []any{"self", cl.UserID, "team"}
	if len(custom) > 0 {
		parts = append(parts, "calendar IN ?")
		args = append(args, custom)
	}
	var todos []models.Todo
	if err := middleware.DB(c).Where(strings.Join(parts, " OR "), args...).
		Order("completed, \"order\", created_at").Find(&todos).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "query_failed")
		return
	}
	view := make([]todoView, 0, len(todos))
	for _, t := range todos {
		view = append(view, toTodoView(t))
	}
	httpx.OK(c, view)
}

type todoInput struct {
	Title     string            `json:"title"`
	Note      string            `json:"note"`
	DueAt     *string           `json:"dueAt"`
	StartAt   *string           `json:"startAt"`
	RRule     string            `json:"rrule"`
	ExDates   []string          `json:"exdates"`
	Group     string            `json:"group"`
	Tags      string            `json:"tags"`
	Priority  int               `json:"priority"`
	Location  string            `json:"location"`
	URL       string            `json:"url"`
	Percent   int               `json:"percent"`
	ParentID  string            `json:"parentId"`
	Order     int               `json:"order"`
	Shared    *bool             `json:"shared"`
	Calendar  string            `json:"calendar"` // "self", "team" or a custom list calendar
	Completed *bool             `json:"completed"`
	Reminders []models.Reminder `json:"reminders"`
	Attendees []string          `json:"attendees"` // team member ids to invite (VTODO ATTENDEE)
}

func parseOptTime(s *string) (*time.Time, bool) {
	if s == nil {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil, false
	}
	u := t.UTC()
	return &u, true
}

// exDatesJoinVal joins RFC3339 strings with a comma for storage in ExDate.
func exDatesJoinVal(ds []string) string {
	parts := make([]string, 0, len(ds))
	for _, d := range ds {
		if d == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, d); err == nil {
			parts = append(parts, t.UTC().Format(time.RFC3339))
		}
	}
	return strings.Join(parts, ",")
}

func normalizeTodo(in *todoInput) bool {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return false
	}
	if in.RRule != "" {
		if _, err := rrule.StrToRRule(in.RRule); err != nil {
			return false
		}
	}
	if _, ok := parseOptTime(in.DueAt); !ok {
		return false
	}
	if _, ok := parseOptTime(in.StartAt); !ok {
		return false
	}
	if in.Priority < 0 || in.Priority > 9 {
		return false
	}
	if in.Percent < 0 || in.Percent > 100 {
		return false
	}
	for _, r := range in.Reminders {
		switch r.Unit {
		case "at":
			if r.At == nil {
				return false
			}
		case "min", "hour", "day":
			if r.Value <= 0 {
				return false
			}
		default:
			return false
		}
	}
	for _, d := range in.ExDates {
		if _, err := time.Parse(time.RFC3339, d); err != nil {
			return false
		}
	}
	return true
}

func (h *Handler) create(c *gin.Context) {
	var in todoInput
	if !httpx.Bind(c, &in) || !normalizeTodo(&in) {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	cl := middleware.ClaimsOf(c)
	// Where does this todo live? Explicit list ("self"/"team"/custom calendar)
	// wins; otherwise the legacy "shared" checkbox promotes to the team list.
	cal := "self"
	switch in.Calendar {
	case "", "self":
		if in.Calendar == "" && in.Shared != nil && *in.Shared {
			cal = "team"
		}
	case "team":
		cal = "team"
	default:
		if !writableTodoCal(middleware.DB(c), cl.TeamID, cl.UserID, in.Calendar) {
			httpx.NotFoundT(c, "todo_list_not_found")
			return
		}
		cal = in.Calendar
	}
	todo := models.Todo{
		ID:       uuid.Must(uuid.NewV7()).String(),
		UID:      "todo-" + uuid.Must(uuid.NewV7()).String(),
		TeamID:   cl.TeamID,
		UserID:   cl.UserID,
		Calendar: cal,
		Shared:   cal != "self",
		Title:    in.Title,
		Note:     in.Note,
		RRule:    in.RRule,
		ExDate:   exDatesJoinVal(in.ExDates),
		Group:    in.Group,
		Tags:     in.Tags,
		Priority: in.Priority,
		Location: in.Location,
		URL:      in.URL,
		Percent:  in.Percent,
		ParentID: in.ParentID,
		Order:    in.Order,
	}
	todo.StartAt, _ = parseOptTime(in.StartAt)
	todo.DueAt, _ = parseOptTime(in.DueAt)
	if len(in.Reminders) > 0 {
		if b, err := json.Marshal(in.Reminders); err == nil {
			todo.Reminders = string(b)
		}
	}
	if in.Completed != nil && *in.Completed {
		todo.Completed = true
		now := time.Now().UTC()
		todo.CompletedAt = &now
		todo.Percent = 100
	}
	// Resolve invited members before writing so the row stores the resolved set.
	attendees, users := modulecalendar.ResolveInvitees(h.app.DB, cl.TeamID, in.Attendees)
	todo.Attendees = models.AttendeesJSON(attendees)
	if err := middleware.DB(c).Create(&todo).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "create_failed")
		return
	}
	// Invitees that cannot already see the list get a private copy in their own
	// personal (self) list, mirroring the event invite flow.
	modulecalendar.SyncTodoInvites(h.app.DB, cl.TeamID, &todo, users, nil, cl.UserID)
	recordTodoLog(middleware.DB(c), cl.TeamID, cl.UserID, todo.ID, "create", "")
	httpx.Created(c, toTodoView(todo))
}

func (h *Handler) update(c *gin.Context) {
	var in todoInput
	if !httpx.Bind(c, &in) || !normalizeTodo(&in) {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	id := c.Param("id")
	cl := middleware.ClaimsOf(c)
	var existing models.Todo
	if err := middleware.DB(c).First(&existing, "id = ?", id).Error; err != nil {
		httpx.NotFoundT(c, "todo_not_found")
		return
	}
	if !canWriteTodo(middleware.DB(c), cl, &existing) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	prev := models.ParseAttendees(existing.Attendees)
	attendees, users := modulecalendar.ResolveInvitees(h.app.DB, cl.TeamID, in.Attendees)
	updates := map[string]any{
		"title":     in.Title,
		"note":      in.Note,
		"r_rule":    in.RRule,
		"ex_date":   exDatesJoinVal(in.ExDates),
		"group":     in.Group,
		"tags":      in.Tags,
		"priority":  in.Priority,
		"location":  in.Location,
		"url":       in.URL,
		"percent":   in.Percent,
		"parent_id": in.ParentID,
		"order":     in.Order,
	}
	if len(in.Reminders) > 0 {
		if b, err := json.Marshal(in.Reminders); err == nil {
			updates["reminders"] = string(b)
		}
	} else {
		updates["reminders"] = ""
	}
	if st, ok := parseOptTime(in.StartAt); ok {
		updates["start_at"] = st
	}
	if du, ok := parseOptTime(in.DueAt); ok {
		updates["due_at"] = du
	}
	// Moving a todo to another list (or back to personal) requires write
	// access to the target list; built-in self/team handle the legacy checkbox.
	// Only the row owner may move it into their personal (self) list.
	if in.Calendar != "" && in.Calendar != existing.Calendar {
		if in.Calendar == "self" && existing.UserID != cl.UserID {
			httpx.ForbiddenT(c, "forbidden")
			return
		}
		if !writableTodoCal(middleware.DB(c), cl.TeamID, cl.UserID, in.Calendar) {
			httpx.NotFoundT(c, "todo_list_not_found")
			return
		}
		updates["calendar"] = in.Calendar
		updates["shared"] = in.Calendar != "self"
	} else if in.Calendar == "" && in.Shared != nil && existing.Calendar == "self" && *in.Shared {
		updates["calendar"] = "team"
		updates["shared"] = true
	} else if in.Calendar == "" && in.Shared != nil && existing.Calendar == "team" && !*in.Shared {
		if existing.UserID != cl.UserID {
			httpx.ForbiddenT(c, "forbidden")
			return
		}
		updates["calendar"] = "self"
		updates["shared"] = false
	}
	if in.Completed != nil {
		updates["completed"] = *in.Completed
		if *in.Completed {
			updates["percent"] = 100
			now := time.Now().UTC()
			updates["completed_at"] = now
		} else {
			updates["completed_at"] = nil
		}
	}
	updates["attendees"] = models.AttendeesJSON(attendees)
	if err := middleware.DB(c).Model(&models.Todo{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	var todo models.Todo
	middleware.DB(c).First(&todo, "id = ?", id)
	// Refresh / prune invitee copies when the attendee set or the list changed.
	modulecalendar.SyncTodoInvites(h.app.DB, cl.TeamID, &todo, users, prev, cl.UserID)
	recordTodoLog(middleware.DB(c), cl.TeamID, cl.UserID, id, "update", "")
	httpx.OK(c, toTodoView(todo))
}

func (h *Handler) toggle(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var todo models.Todo
	if err := middleware.DB(c).First(&todo, "id = ?", c.Param("id")).Error; err != nil {
		httpx.NotFoundT(c, "todo_not_found")
		return
	}
	if !canWriteTodo(middleware.DB(c), cl, &todo) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	updates := map[string]any{"completed": !todo.Completed}
	if !todo.Completed {
		updates["percent"] = 100
		now := time.Now().UTC()
		updates["completed_at"] = now
	} else {
		updates["percent"] = 0
		updates["completed_at"] = nil
	}
	if err := middleware.DB(c).Model(&todo).Updates(updates).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	recordTodoLog(middleware.DB(c), cl.TeamID, cl.UserID, todo.ID, "toggle", "")
	var updated models.Todo
	middleware.DB(c).First(&updated, "id = ?", c.Param("id"))
	httpx.OK(c, toTodoView(updated))
}

func (h *Handler) delete(c *gin.Context) {
	id := c.Param("id")
	cl := middleware.ClaimsOf(c)
	var todo models.Todo
	if err := middleware.DB(c).First(&todo, "id = ?", id).Error; err != nil {
		httpx.NotFoundT(c, "todo_not_found")
		return
	}
	if !canWriteTodo(middleware.DB(c), cl, &todo) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	if err := middleware.DB(c).Where("id = ?", id).Delete(&models.Todo{}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "delete_failed")
		return
	}
	// Cascade: drop the private copies every invitee held.
	if prev := models.ParseAttendees(todo.Attendees); len(prev) > 0 {
		ids := make([]string, 0, len(prev))
		for _, a := range prev {
			ids = append(ids, a.ID)
		}
		_ = modulecalendar.SoftDeleteTodoInviteeCopies(middleware.DB(c), todo.UID, ids)
	}
	recordTodoLog(middleware.DB(c), cl.TeamID, cl.UserID, id, "delete", "")
	httpx.OK(c, gin.H{"ok": true})
}

// accessibleTodoCalNames returns the names of the custom calendars (excluding
// built-in self/team) whose todos the user may read.
func accessibleTodoCalNames(db *gorm.DB, cl *middleware.Claims) []string {
	cals := modulecalendar.TodoListsForUser(db, cl.TeamID, cl.UserID)
	names := make([]string, 0, len(cals))
	for i := range cals {
		names = append(names, cals[i].Name)
	}
	return names
}

// canWriteTodo reports whether the user may edit/toggle/delete a todo row:
// their own personal todos, anything in the built-in team calendar, and the
// todos of custom calendars the user can access.
func canWriteTodo(db *gorm.DB, cl *middleware.Claims, t *models.Todo) bool {
	switch t.Calendar {
	case "self":
		return t.UserID == cl.UserID
	case "team":
		return true
	}
	return modulecalendar.TodoListRole(db, cl.TeamID, cl.UserID, t.Calendar) != ""
}

// writableTodoCal reports whether todos may be created in / moved into a
// calendar (built-in or an accessible custom list).
func writableTodoCal(db *gorm.DB, teamID, userID, calName string) bool {
	if calName == "self" || calName == "team" {
		return true
	}
	return modulecalendar.TodoListRole(db, teamID, userID, calName) != ""
}
