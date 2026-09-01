package moduletodos

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/teambition/rrule-go"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	"homihub/backend/internal/modules"
)

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
}

type todoView struct {
	models.Todo
	HasDate   bool              `json:"hasDate"`
	Reminders []models.Reminder `json:"reminders"`
	ExDates   []string          `json:"exdates"`
}

func toTodoView(t models.Todo) todoView {
	return todoView{
		Todo:      t,
		HasDate:   t.DueAt != nil,
		Reminders: models.ParseReminders(t.Reminders),
		ExDates:   exDatesToStrs(models.ExDateSplit(t.ExDate)),
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

// list returns the current user's private todos plus todos shared to the family.
func (h *Handler) list(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var todos []models.Todo
	if err := middleware.DB(c).Where("user_id = ? OR shared = ?", cl.UserID, true).
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
	Completed *bool             `json:"completed"`
	Reminders []models.Reminder `json:"reminders"`
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
	todo := models.Todo{
		ID:       uuid.Must(uuid.NewV7()).String(),
		UID:      "todo-" + uuid.Must(uuid.NewV7()).String(),
		TeamID:   cl.TeamID,
		UserID:   cl.UserID,
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
	if in.Shared != nil {
		todo.Shared = *in.Shared
	}
	if in.Completed != nil && *in.Completed {
		todo.Completed = true
		now := time.Now().UTC()
		todo.CompletedAt = &now
		todo.Percent = 100
	}
	if err := middleware.DB(c).Create(&todo).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "create_failed")
		return
	}
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
	if existing.UserID != cl.UserID && !(existing.Shared && cl.Role == middleware.RoleParent) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	updates := map[string]any{
		"title":    in.Title,
		"note":     in.Note,
		"r_rule":   in.RRule,
		"ex_date":  exDatesJoinVal(in.ExDates),
		"group":    in.Group,
		"tags":     in.Tags,
		"priority": in.Priority,
		"location": in.Location,
		"url":      in.URL,
		"percent":  in.Percent,
		"parent_id": in.ParentID,
		"order":    in.Order,
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
	if in.Shared != nil {
		updates["shared"] = *in.Shared
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
	if err := middleware.DB(c).Model(&models.Todo{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	var todo models.Todo
	middleware.DB(c).First(&todo, "id = ?", id)
	httpx.OK(c, toTodoView(todo))
}

func (h *Handler) toggle(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var todo models.Todo
	if err := middleware.DB(c).First(&todo, "id = ?", c.Param("id")).Error; err != nil {
		httpx.NotFoundT(c, "todo_not_found")
		return
	}
	if todo.UserID != cl.UserID && !(todo.Shared && cl.Role == middleware.RoleParent) {
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
	if todo.UserID != cl.UserID && !(todo.Shared && cl.Role == middleware.RoleParent) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	if err := middleware.DB(c).Where("id = ?", id).Delete(&models.Todo{}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "delete_failed")
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}
