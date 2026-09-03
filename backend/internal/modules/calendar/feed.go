package modulecalendar

import (
	"fmt"
	"net/http"
	"time"

	"github.com/emersion/go-ical"
	"github.com/gin-gonic/gin"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
)

// feedSelf serves the personal (self) iCal subscription feed at
// GET /calendar/feed.ics. The token is the subscriber's App Password — no
// account in the URL. It returns the member's personal calendar: team-visible
// events of every member, the subscriber's own private events, busy events of
// others (masked) + their own dated todos.
func (h *Handler) feedSelf(c *gin.Context) {
	raw := c.Query("token")
	if raw == "" {
		httpx.UnauthorizedT(c, "subscription_token_invalid")
		return
	}
	var tok models.Token
	if err := h.app.DB.Where("token_hash = ? AND kind = ? AND revoked_at IS NULL",
		middleware.HashToken(raw), "app_password").First(&tok).Error; err != nil {
		httpx.UnauthorizedT(c, "subscription_token_invalid")
		return
	}
	var user models.User
	if err := h.app.DB.First(&user, "id = ?", tok.SubjectID).Error; err != nil {
		httpx.UnauthorizedT(c, "subscription_token_invalid")
		return
	}
	var fam models.Team
	if err := h.app.DB.First(&fam, "id = ?", user.TeamID).Error; err != nil {
		httpx.UnauthorizedT(c, "subscription_token_invalid")
		return
	}
	var uf models.TeamMember
	if err := h.app.DB.Where("user_id = ? AND team_id = ?", user.ID, fam.ID).First(&uf).Error; err != nil {
		httpx.UnauthorizedT(c, "subscription_token_invalid")
		return
	}
	// Personal scope: team-visible events of every member, own private events,
	// busy events (masked below) + own dated todos.
	var evs []models.CalendarEvent
	if err := h.app.DB.Where(
		"team_id = ? AND (visibility = ? OR (visibility = ? AND user_id = ?) OR visibility = ?)",
		fam.ID, models.VisibilityTeam, models.VisibilityPrivate, user.ID, models.VisibilityBusy,
	).Order("starts_at").Find(&evs).Error; err != nil {
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
	var todos []models.Todo
	if err := h.app.DB.Where("team_id = ? AND user_id = ? AND calendar = ?", fam.ID, user.ID, calSelf).
		Order("due_at").Find(&todos).Error; err == nil {
		for i := range todos {
			cal.Children = append(cal.Children, todoComponent(&todos[i]))
		}
	}
	c.Data(http.StatusOK, "text/calendar; charset=utf-8", serialize(cal))
}

// feedTeam serves the team's shared iCal feed at GET /calendar/team.ics. The
// token is the team's calendar token. It returns team-visible/busy events +
// team todos only — no member's personal (self) events are included.
func (h *Handler) feedTeam(c *gin.Context) {
	var fam models.Team
	if err := h.app.DB.Where("calendar_token = ?", c.Query("token")).First(&fam).Error; err != nil {
		httpx.UnauthorizedT(c, "subscription_token_invalid")
		return
	}
	// Team scope: team-visible/busy events + team todos.
	var evs []models.CalendarEvent
	if err := h.app.DB.Where("team_id = ? AND visibility IN ?", fam.ID,
		[]int{models.VisibilityTeam, models.VisibilityBusy}).Order("starts_at").Find(&evs).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "query_failed")
		return
	}
	cal := BuildCalendar(fam.Name, evs)
	var todos []models.Todo
	if err := h.app.DB.Where("team_id = ? AND calendar = ?", fam.ID, calTeam).
		Order("due_at").Find(&todos).Error; err == nil {
		for i := range todos {
			cal.Children = append(cal.Children, todoComponent(&todos[i]))
		}
	}
	c.Data(http.StatusOK, "text/calendar; charset=utf-8", serialize(cal))
}

func todoComponent(t *models.Todo) *ical.Component {
	comp := ical.NewComponent(ical.CompToDo)
	comp.Props.SetText(ical.PropUID, todoUID(t))
	comp.Props.SetText(ical.PropSummary, t.Title)
	comp.Props.SetDateTime(ical.PropDateTimeStamp, t.UpdatedAt)
	if t.StartAt != nil {
		comp.Props.SetDateTime(ical.PropDateTimeStart, *t.StartAt)
	}
	if t.DueAt != nil {
		comp.Props.SetDateTime(ical.PropDue, *t.DueAt)
	}
	if t.Location != "" {
		comp.Props.SetText(ical.PropLocation, t.Location)
	}
	if t.URL != "" {
		comp.Props.SetText(ical.PropURL, t.URL)
	}
	if t.Note != "" {
		comp.Props.SetText(ical.PropDescription, t.Note)
	}
	if t.Group != "" {
		comp.Props.SetText(ical.PropCategories, t.Group)
	}
	if t.Tags != "" {
		if cat, err := comp.Props.Text(ical.PropCategories); err == nil {
			comp.Props.SetText(ical.PropCategories, cat+","+t.Tags)
		}
	}
	if t.ParentID != "" {
		comp.Props.SetText(ical.PropRelatedTo, t.ParentID)
	}
	if t.Completed {
		comp.Props.SetText(ical.PropStatus, "COMPLETED")
		comp.Props.SetText(ical.PropPercentComplete, "100")
		if t.CompletedAt != nil {
			comp.Props.SetDateTime(ical.PropCompleted, *t.CompletedAt)
		}
	} else {
		status := "NEEDS-ACTION"
		if t.Percent > 0 {
			status = "IN-PROCESS"
		}
		comp.Props.SetText(ical.PropStatus, status)
		comp.Props.SetText(ical.PropPercentComplete, fmt.Sprintf("%d", t.Percent))
	}
	if t.Priority > 0 {
		comp.Props.SetText(ical.PropPriority, fmt.Sprintf("%d", t.Priority))
	}
	if t.RRule != "" {
		p := ical.NewProp(ical.PropRecurrenceRule)
		p.Value = t.RRule
		comp.Props.Set(p)
	}
	writeAttendeeProps(comp, models.ParseAttendees(t.Attendees))
	writeExDates(comp, exDatesSplit(t.ExDate))
	if rem := parseRemindersJSON(t.Reminders); len(rem) > 0 {
		base := time.Now()
		if t.StartAt != nil {
			base = *t.StartAt
		} else if t.DueAt != nil {
			base = *t.DueAt
		}
		addAlarms(comp, base, rem)
	}
	return comp
}
