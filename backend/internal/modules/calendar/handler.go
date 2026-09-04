package modulecalendar

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/emersion/go-ical"
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
	g.GET("/calendar/feed.ics", h.feedSelf) // personal (self) calendar — token = App Password
	g.GET("/calendar/team.ics", h.feedTeam)  // team shared calendar — token = Calendar Token
}

type eventInput struct {
	Title       string            `json:"title"`
	Category    string            `json:"category"`
	Location    string            `json:"location"`
	Description string            `json:"description"`
	StartsAt    string            `json:"startsAt"`
	EndsAt      string            `json:"endsAt"`
	AllDay      bool              `json:"allDay"`
	RRule       string            `json:"rrule"`
	ExDates     []string          `json:"exdates"`
	Visibility  int               `json:"visibility"`
	Reminders   []models.Reminder `json:"reminders"`
	Attendees   []string          `json:"attendees"` // team member ids to invite
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
	if input.Visibility < models.VisibilityPrivate || input.Visibility > models.VisibilityTeam {
		input.Visibility = models.VisibilityTeam
	}
	for _, r := range input.Reminders {
		switch r.Unit {
		case "at":
			if r.At == nil {
				return time.Time{}, time.Time{}, false
			}
		case "min", "hour", "day":
			if r.Value <= 0 {
				return time.Time{}, time.Time{}, false
			}
		default:
			return time.Time{}, time.Time{}, false
		}
	}
	for _, d := range input.ExDates {
		if _, err := time.Parse(time.RFC3339, d); err != nil {
			return time.Time{}, time.Time{}, false
		}
	}
	return start, end, true
}

type eventView struct {
	models.CalendarEvent
	Start     time.Time          `json:"start"`
	End       time.Time          `json:"end"`
	Single    bool               `json:"single"`
	Source    string             `json:"source,omitempty"`
	Reminders []models.Reminder  `json:"reminders"`
	ExDates   []string           `json:"exdates"`
	Attendees []models.Attendee  `json:"attendees"`
}

// exDatesJoinValEx joins RFC3339 strings with a comma for the ExDate column.
func exDatesJoinValEx(ds []string) string {
	dates := make([]time.Time, 0, len(ds))
	for _, d := range ds {
		if t, err := time.Parse(time.RFC3339, d); err == nil {
			dates = append(dates, t.UTC())
		}
	}
	return exDatesJoin(dates)
}

// remindersJSON serializes a reminder list to the model's JSON column.
func remindersJSON(rs []models.Reminder) string {
	if len(rs) == 0 {
		return ""
	}
	b, err := json.Marshal(rs)
	if err != nil {
		return ""
	}
	return string(b)
}

// exDatesSplitStrs returns the model's ExDate values as RFC3339 strings.
func exDatesSplitStrs(s string) []string {
	dates := exDatesSplit(s)
	out := make([]string, 0, len(dates))
	for _, d := range dates {
		out = append(out, d.UTC().Format(time.RFC3339))
	}
	return out
}

func occurrenceView(ev *models.CalendarEvent, start, end time.Time) eventView {
	return eventView{
		CalendarEvent: *ev,
		Start:         start,
		End:           end,
		Single:        ev.RRule == "",
		Reminders:     parseRemindersJSON(ev.Reminders),
		ExDates:       exDatesSplitStrs(ev.ExDate),
		Attendees:     models.ParseAttendees(ev.Attendees),
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
		models.VisibilityTeam, models.VisibilityPrivate, cl.UserID, models.VisibilityBusy,
	).Where("component_type <> ?", ical.CompJournal).Find(&evs).Error; err != nil {
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

// personalTodoOccurrences expands the current user's personal dated todos into
// calendar occurrences so they surface in the personal calendar.
func (h *Handler) personalTodoOccurrences(c *gin.Context, from, to time.Time) []eventView {
	cl := middleware.ClaimsOf(c)
	var todos []models.Todo
	if err := middleware.DB(c).Where("user_id = ? AND calendar = ? AND due_at IS NOT NULL", cl.UserID, calSelf).Find(&todos).Error; err != nil {
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
						Calendar:  calSelf,
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
		excluded := exDateSet(todo.ExDate)
		for _, start := range rr.Between(from, to, true) {
			start = start.UTC()
			if excluded[start] {
				continue
			}
			end := start.Add(time.Hour)
			out = append(out, eventView{
				CalendarEvent: models.CalendarEvent{
					ID: todo.ID, UserID: todo.UserID, Title: todo.Title,
					Description: todo.Note, StartsAt: start, EndsAt: end, RRule: todo.RRule,
					Visibility: models.VisibilityPrivate, Category: "family",
					Calendar:  calSelf,
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
	excluded := exDateSet(ev.ExDate)
	exceptions, _ := loadExceptions(db, ev.UID)
	view := make([]eventView, 0, len(occ))
	for _, start := range occ {
		start = start.UTC()
		if excluded[start] {
			continue
		}
		occEnd := start.Add(ev.EndsAt.Sub(ev.StartsAt))
		if ex, ok := exceptions[start]; ok {
			view = append(view, occurrenceView(ev, ex.StartsAt, ex.EndsAt))
			continue
		}
		view = append(view, occurrenceView(ev, start, occEnd))
	}
	return view
}

// exDateSet builds a lookup set keyed by UTC date-time for EXDATE exclusion.
func exDateSet(s string) map[time.Time]bool {
	dates := exDatesSplit(s)
	m := make(map[time.Time]bool, len(dates))
	for _, d := range dates {
		m[d] = true
	}
	return m
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
	// Resolve invited members before writing so the row stores the resolved set.
	attendees, users := ResolveInvitees(h.app.DB, cl.TeamID, in.Attendees)
	ev := models.CalendarEvent{
		ID:          uuid.Must(uuid.NewV7()).String(),
		TeamID:      cl.TeamID,
		UserID:      cl.UserID,
		UID:         uuid.Must(uuid.NewV7()).String(),
		Title:       in.Title,
		Category:    in.Category,
		Location:    in.Location,
		Description: in.Description,
		StartsAt:    startsAt.UTC(),
		EndsAt:      endsAt.UTC(),
		AllDay:      in.AllDay,
		RRule:       in.RRule,
		ExDate:      exDatesJoinValEx(in.ExDates),
		Visibility:  in.Visibility,
		Calendar:    calForVisibility(in.Visibility),
		Attendees:   models.AttendeesJSON(attendees),
	}
	if len(in.Reminders) > 0 {
		if b, err := json.Marshal(in.Reminders); err == nil {
			ev.Reminders = string(b)
		}
	}
	if err := middleware.DB(c).Create(&ev).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "create_failed")
		return
	}
	// Invitees that cannot already see the event get a private personal copy.
	SyncEventInvites(h.app.DB, cl.TeamID, &ev, users, nil, cl.UserID)
	h.recordEventSync(&ev, false)
	httpx.Created(c, occurrenceView(&ev, ev.StartsAt, ev.EndsAt))
}

// calForVisibility maps an event visibility to its calendar name.
func calForVisibility(v int) string {
	if v == models.VisibilityPrivate {
		return calSelf
	}
	return calTeam
}

// recordEventSync bumps the CalDAV sync log for an event created/updated via
// the REST API so external clients see it through sync-collection.
func (h *Handler) recordEventSync(ev *models.CalendarEvent, deleted bool) {
	cal := ev.Calendar
	if cal == "" {
		cal = calForVisibility(ev.Visibility)
	}
	// Determine the calendar via the event's ownership and visibility.
	user := models.User{}
	if h.app.DB.Where("id = ?", ev.UserID).First(&user).Error == nil {
		href := calendarPath(user.Email, cal) + ev.UID + ".ics"
		etag := ""
		if !deleted {
			etag = eventEtag(ev)
		}
		_, _ = syncLogChange(h.app.DB, ev.TeamID, cal, href, etag, deleted)
	}
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
	prev := models.ParseAttendees(existing.Attendees)
	attendees, users := ResolveInvitees(h.app.DB, cl.TeamID, in.Attendees)
	if err := middleware.DB(c).Model(&models.CalendarEvent{}).Where("id = ?", id).Updates(map[string]any{
		"title":       in.Title,
		"category":    in.Category,
		"location":    in.Location,
		"description": in.Description,
		"starts_at":   startsAt.UTC(),
		"ends_at":     endsAt.UTC(),
		"all_day":     in.AllDay,
		"r_rule":      in.RRule,
		"ex_date":     exDatesJoinValEx(in.ExDates),
		"visibility":  in.Visibility,
		"calendar":    calForVisibility(in.Visibility),
		"reminders":   remindersJSON(in.Reminders),
		"attendees":   models.AttendeesJSON(attendees),
	}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	var ev models.CalendarEvent
	middleware.DB(c).First(&ev, "id = ?", id)
	SyncEventInvites(h.app.DB, cl.TeamID, &ev, users, prev, cl.UserID)
	h.recordEventSync(&ev, false)
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
	// Soft delete: the row keeps a deleted_at marker and lives in the items
	// trash until purged. Invitee copies are soft-deleted in cascade.
	if err := middleware.DB(c).Where("id = ?", id).Delete(&models.CalendarEvent{}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "delete_failed")
		return
	}
	// Cascade: drop the private copies every invitee held.
	if prev := models.ParseAttendees(ev.Attendees); len(prev) > 0 {
		ids := make([]string, 0, len(prev))
		for _, a := range prev {
			ids = append(ids, a.ID)
		}
		_ = SoftDeleteEventInviteeCopies(middleware.DB(c), ev.UID, ids)
	}
	h.recordEventSync(&ev, true)
	httpx.OK(c, gin.H{"ok": true})
}
