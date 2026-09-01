package modulecalendar

import (
	"net/http"
	"time"

	"github.com/emersion/go-ical"
	"github.com/gin-gonic/gin"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/models"
)

// feed serves an iCal subscription feed. Supports family-level auth via
// ?token=<family calendar_token> and member-level auth via Basic Auth
// (email + family calendar_token).
func (h *Handler) feed(c *gin.Context) {
	if email, pass, ok := c.Request.BasicAuth(); ok && email != "" && pass != "" {
		h.feedAsMember(c, email, pass)
		return
	}
	h.feedAsTeam(c)
}

func (h *Handler) feedAsTeam(c *gin.Context) {
	var fam models.Team
	if err := h.app.DB.Where("calendar_token = ?", c.Query("token")).First(&fam).Error; err != nil {
		httpx.UnauthorizedT(c, "calendar_token_invalid")
		return
	}
	scope := c.DefaultQuery("scope", "team")
	var evs []models.CalendarEvent
	q := h.app.DB.Where("team_id = ?", fam.ID)
	switch {
	case scope == "self":
		q = q.Where("visibility IN ?", []int{models.VisibilityPrivate, models.VisibilityBusy, models.VisibilityTeam})
	default:
		q = q.Where("visibility IN ?", []int{models.VisibilityTeam, models.VisibilityBusy})
	}
	if err := q.Order("starts_at").Find(&evs).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "query_failed")
		return
	}
	cal := BuildCalendar(fam.Name, evs)
	c.Data(http.StatusOK, "text/calendar; charset=utf-8", serialize(cal))
}

func (h *Handler) feedAsMember(c *gin.Context, email, calendarToken string) {
	var fam models.Team
	if err := h.app.DB.Where("calendar_token = ?", calendarToken).First(&fam).Error; err != nil {
		httpx.UnauthorizedT(c, "calendar_token_invalid")
		return
	}
	var user models.User
	if err := h.app.DB.Where("email = ?", email).First(&user).Error; err != nil {
		httpx.UnauthorizedT(c, "calendar_token_invalid")
		return
	}
	var uf models.TeamMember
	if err := h.app.DB.Where("user_id = ? AND team_id = ?", user.ID, fam.ID).First(&uf).Error; err != nil {
		httpx.UnauthorizedT(c, "calendar_token_invalid")
		return
	}
	scope := c.DefaultQuery("scope", "self")
	q := h.app.DB.Where("team_id = ?", fam.ID)
	if scope == "team" {
		q = q.Where("visibility IN ?", []int{models.VisibilityTeam, models.VisibilityBusy})
	} else {
		q = q.Where(
			"visibility = ? OR (visibility = ? AND user_id = ?) OR visibility = ?",
			models.VisibilityTeam, models.VisibilityPrivate, user.ID, models.VisibilityBusy,
		)
	}
	var evs []models.CalendarEvent
	if err := q.Order("starts_at").Find(&evs).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "query_failed")
		return
	}
	for i := range evs {
		if evs[i].Visibility == models.VisibilityBusy && evs[i].UserID != user.ID {
			evs[i].Title = "忙碌"
			evs[i].Location = ""
			evs[i].Description = ""
		}
	}
	cal := BuildCalendar(fam.Name, evs)
	if scope == "self" {
		var todos []models.Todo
		if err := h.app.DB.Where("team_id = ? AND user_id = ? AND due_at IS NOT NULL", fam.ID, user.ID).
			Order("due_at").Find(&todos).Error; err == nil {
			for i := range todos {
				cal.Children = append(cal.Children, todoComponent(&todos[i]))
			}
		}
	}
	c.Data(http.StatusOK, "text/calendar; charset=utf-8", serialize(cal))
}

func todoComponent(t *models.Todo) *ical.Component {
	comp := ical.NewEvent()
	comp.Props.SetText(ical.PropUID, "todo-"+t.ID)
	comp.Props.SetText(ical.PropSummary, t.Title)
	comp.Props.SetDateTime(ical.PropDateTimeStamp, t.UpdatedAt)
	comp.Props.SetDateTime(ical.PropDateTimeStart, *t.DueAt)
	comp.Props.SetDateTime(ical.PropDateTimeEnd, t.DueAt.Add(time.Hour))
	comp.Props.SetText("X-HOMIHUB-TYPE", "todo")
	if t.Note != "" {
		comp.Props.SetText(ical.PropDescription, t.Note)
	}
	if t.RRule != "" {
		p := ical.NewProp(ical.PropRecurrenceRule)
		p.Value = t.RRule
		comp.Props.Set(p)
	}
	return comp.Component
}
