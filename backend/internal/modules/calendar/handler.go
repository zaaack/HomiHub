package modulecalendar

import (
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
)

var Categories = map[string]string{
	"work":   "工作",
	"school": "学校",
	"family": "家庭",
}

type Handler struct {
	app *modules.App
}

func (h *Handler) ID() string { return "calendar" }

func (h *Handler) Init(app *modules.App) error {
	h.app = app
	return nil
}

func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	auth := middleware.Auth(h.app.DB, h.app.Config.JWTSecret)
	g.GET("/events", auth, h.list)
	g.POST("/events", auth, middleware.RequireWriteRole(), h.create)
	g.PUT("/events/:id", auth, middleware.RequireWriteRole(), h.update)
	g.DELETE("/events/:id", auth, middleware.RequireWriteRole(), h.delete)
	g.GET("/calendar/feed.ics", h.feed)
}

type eventInput struct {
	Title       string `json:"title"`
	Category    string `json:"category"`
	Location    string `json:"location"`
	Description string `json:"description"`
	StartsAt    string `json:"startsAt"`
	EndsAt      string `json:"endsAt"`
	AllDay      bool   `json:"allDay"`
	RRule       string `json:"rrule"`
	Visibility  int    `json:"visibility"`
}

func normalize(input *eventInput) (time.Time, time.Time, bool) {
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" {
		return time.Time{}, time.Time{}, false
	}
	start, err1 := time.Parse(time.RFC3339, input.StartsAt)
	end, err2 := time.Parse(time.RFC3339, input.EndsAt)
	if err1 != nil || err2 != nil || end.Before(start) {
		return time.Time{}, time.Time{}, false
	}
	if _, ok := Categories[input.Category]; !ok {
		input.Category = "family"
	}
	if input.RRule != "" {
		rr, err := rrule.StrToRRule(input.RRule)
		if err != nil || rr.OrigOptions.Freq > rrule.DAILY {
			return time.Time{}, time.Time{}, false
		}
	}
	if input.Visibility < models.VisibilityPrivate || input.Visibility > models.VisibilityFamily {
		input.Visibility = models.VisibilityFamily
	}
	return start, end, true
}

type eventView struct {
	models.CalendarEvent
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Single bool      `json:"single"`
	Source string    `json:"source,omitempty"`
}

func occurrenceView(ev *models.CalendarEvent, start, end time.Time) eventView {
	return eventView{
		CalendarEvent: *ev,
		Start:         start,
		End:           end,
		Single:        ev.RRule == "",
	}
}

func (h *Handler) list(c *gin.Context) {
	from, err1 := time.Parse(time.RFC3339, c.Query("from"))
	to, err2 := time.Parse(time.RFC3339, c.Query("to"))
	if err1 != nil || err2 != nil || !to.After(from) {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	cl := middleware.ClaimsOf(c)
	var evs []models.CalendarEvent
	if err := middleware.DB(c).Where(
		"visibility = ? OR (visibility = ? AND user_id = ?) OR visibility = ?",
		models.VisibilityFamily, models.VisibilityPrivate, cl.UserID, models.VisibilityBusy,
	).Find(&evs).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "query_failed")
		return
	}
	view := []eventView{}
	for i := range evs {
		ev := &evs[i]
		if ev.Visibility == models.VisibilityBusy && ev.UserID != cl.UserID {
			redacted := *ev
			redacted.Title = "忙碌"
			redacted.Location = ""
			redacted.Description = ""
			ev = &redacted
		}
		if ev.RecurrenceID != nil {
			continue
		}
		if ev.RRule == "" {
			if !ev.EndsAt.Before(from) && !ev.StartsAt.After(to) {
				view = append(view, occurrenceView(ev, ev.StartsAt, ev.EndsAt))
			}
			continue
		}
		origEv := &evs[i]
		expanded := h.expand(middleware.DB(c), origEv, from, to)
		for j := range expanded {
			if ev.Visibility == models.VisibilityBusy && origEv.UserID != cl.UserID {
				expanded[j].Title = "忙碌"
				expanded[j].Location = ""
				expanded[j].Description = ""
			}
		}
		view = append(view, expanded...)
	}
	view = append(view, h.personalTodoOccurrences(c, from, to)...)
	httpx.OK(c, view)
}

// personalTodoOccurrences expands the current user's dated todos into calendar
// occurrences so they surface in the personal calendar.
func (h *Handler) personalTodoOccurrences(c *gin.Context, from, to time.Time) []eventView {
	cl := middleware.ClaimsOf(c)
	var todos []models.Todo
	if err := middleware.DB(c).Where("user_id = ? AND due_at IS NOT NULL", cl.UserID).Find(&todos).Error; err != nil {
		return nil
	}
	out := []eventView{}
	for i := range todos {
		todo := &todos[i]
		if todo.RRule == "" {
			if !todo.DueAt.After(to) && !todo.DueAt.Before(from.Add(-24*time.Hour)) {
				end := todo.DueAt.Add(time.Hour)
				out = append(out, eventView{
					CalendarEvent: models.CalendarEvent{
						ID: todo.ID, UserID: todo.UserID, Title: todo.Title,
						Description: todo.Note, StartsAt: *todo.DueAt, EndsAt: end,
						Visibility: models.VisibilityPrivate, Category: "family",
						CreatedAt: todo.CreatedAt, UpdatedAt: todo.UpdatedAt,
					},
					Start: *todo.DueAt, End: end, Single: true, Source: "todo",
				})
			}
			continue
		}
		rr, err := parseRuleWithStart(todo.RRule, *todo.DueAt)
		if err != nil {
			continue
		}
		for _, start := range rr.Between(from, to, true) {
			start = start.UTC()
			end := start.Add(time.Hour)
			out = append(out, eventView{
				CalendarEvent: models.CalendarEvent{
					ID: todo.ID, UserID: todo.UserID, Title: todo.Title,
					Description: todo.Note, StartsAt: start, EndsAt: end, RRule: todo.RRule,
					Visibility: models.VisibilityPrivate, Category: "family",
					CreatedAt: todo.CreatedAt, UpdatedAt: todo.UpdatedAt,
				},
				Start: start, End: end, Single: false, Source: "todo",
			})
		}
	}
	return out
}

func (h *Handler) expand(db *gorm.DB, ev *models.CalendarEvent, from, to time.Time) []eventView {
	rr, err := parseRuleWithStart(ev.RRule, ev.StartsAt)
	if err != nil {
		return nil
	}
	occ := rr.Between(from, to, true)
	exceptions, _ := loadExceptions(db, ev.UID)
	view := make([]eventView, 0, len(occ))
	for _, start := range occ {
		start = start.UTC()
		occEnd := start.Add(ev.EndsAt.Sub(ev.StartsAt))
		if ex, ok := exceptions[start]; ok {
			view = append(view, occurrenceView(ev, ex.StartsAt, ex.EndsAt))
			continue
		}
		view = append(view, occurrenceView(ev, start, occEnd))
	}
	return view
}

func loadExceptions(db *gorm.DB, uid string) (map[time.Time]*models.CalendarEvent, error) {
	var exs []models.CalendarEvent
	if err := db.Where("uid = ? AND recurrence_id IS NOT NULL", uid).Find(&exs).Error; err != nil {
		return nil, err
	}
	m := make(map[time.Time]*models.CalendarEvent, len(exs))
	for i := range exs {
		m[exs[i].RecurrenceID.UTC()] = &exs[i]
	}
	return m, nil
}

func (h *Handler) create(c *gin.Context) {
	var in eventInput
	if !httpx.Bind(c, &in) {
		return
	}
	startsAt, endsAt, ok := normalize(&in)
	if !ok {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	cl := middleware.ClaimsOf(c)
	ev := models.CalendarEvent{
		ID:         uuid.Must(uuid.NewV7()).String(),
		FamilyID:   cl.FamilyID,
		UserID:     cl.UserID,
		UID:        uuid.Must(uuid.NewV7()).String(),
		Title:      in.Title,
		Category:   in.Category,
		Location:   in.Location,
		Description: in.Description,
		StartsAt:   startsAt.UTC(),
		EndsAt:     endsAt.UTC(),
		AllDay:     in.AllDay,
		RRule:      in.RRule,
		Visibility: in.Visibility,
	}
	if err := middleware.DB(c).Create(&ev).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "create_failed")
		return
	}
	httpx.Created(c, occurrenceView(&ev, ev.StartsAt, ev.EndsAt))
}

func (h *Handler) update(c *gin.Context) {
	var in eventInput
	if !httpx.Bind(c, &in) {
		return
	}
	startsAt, endsAt, ok := normalize(&in)
	if !ok {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	id := c.Param("id")
	cl := middleware.ClaimsOf(c)
	var existing models.CalendarEvent
	if err := middleware.DB(c).First(&existing, "id = ?", id).Error; err != nil {
		httpx.NotFoundT(c, "event_not_found")
		return
	}
	if existing.UserID != cl.UserID && cl.Role != middleware.RoleParent {
		httpx.ForbiddenT(c, "edit_own_only")
		return
	}
	if err := middleware.DB(c).Model(&models.CalendarEvent{}).Where("id = ?", id).Updates(map[string]any{
		"title":       in.Title,
		"category":    in.Category,
		"location":    in.Location,
		"description": in.Description,
		"starts_at":   startsAt.UTC(),
		"ends_at":     endsAt.UTC(),
		"all_day":     in.AllDay,
		"r_rule":      in.RRule,
		"visibility":  in.Visibility,
	}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	var ev models.CalendarEvent
	middleware.DB(c).First(&ev, "id = ?", id)
	httpx.OK(c, occurrenceView(&ev, ev.StartsAt, ev.EndsAt))
}

func (h *Handler) delete(c *gin.Context) {
	id := c.Param("id")
	cl := middleware.ClaimsOf(c)
	var ev models.CalendarEvent
	if err := middleware.DB(c).First(&ev, "id = ?", id).Error; err != nil {
		httpx.NotFoundT(c, "event_not_found")
		return
	}
	if ev.UserID != cl.UserID && cl.Role != middleware.RoleParent {
		httpx.ForbiddenT(c, "delete_own_only")
		return
	}
	if err := middleware.DB(c).Where("id = ?", id).Delete(&models.CalendarEvent{}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "delete_failed")
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}
