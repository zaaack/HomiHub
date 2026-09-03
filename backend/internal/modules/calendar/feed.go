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
	if scope == "self" {
		var todos []models.Todo
		if err := h.app.DB.Where("team_id = ? AND user_id = ? AND calendar = ?", fam.ID, fam.OwnerID, calSelf).
			Order("due_at").Find(&todos).Error; err == nil {
			for i := range todos {
				cal.Children = append(cal.Children, todoComponent(&todos[i]))
			}
		}
	} else {
		var todos []models.Todo
		if err := h.app.DB.Where("team_id = ? AND calendar = ?", fam.ID, calTeam).
			Order("due_at").Find(&todos).Error; err == nil {
			for i := range todos {
				cal.Children = append(cal.Children, todoComponent(&todos[i]))
			}
		}
	}
	c.Data(http.StatusOK, "text/calendar; charset=utf-8", serialize(cal))
}

func (h *Handler) feedAsMember(c *gin.Context, email, pass string) {
	var fam models.Team
	var user models.User
	authed := false
	// 1. family calendar_token
	if h.app.DB.Where("calendar_token = ?", pass).First(&fam).Error == nil {
		authed = true
	}
	// 2. app_password (personal token)
	if !authed && h.app.DB.Where("email = ?", email).First(&user).Error == nil {
		var tok models.Token
		if h.app.DB.Where("token_hash = ? AND kind = ? AND subject_id = ? AND revoked_at IS NULL",
			middleware.HashToken(pass), "app_password", user.ID).First(&tok).Error == nil {
			if err := h.app.DB.Where("id = ?", user.TeamID).First(&fam).Error; err == nil {
				authed = true
			}
		}
	}
	if !authed {
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
		if err := h.app.DB.Where("team_id = ? AND user_id = ? AND calendar = ?", fam.ID, user.ID, calSelf).
			Order("due_at").Find(&todos).Error; err == nil {
			for i := range todos {
				cal.Children = append(cal.Children, todoComponent(&todos[i]))
			}
		}
	} else {
		var todos []models.Todo
		if err := h.app.DB.Where("team_id = ? AND calendar = ?", fam.ID, calTeam).
			Order("due_at").Find(&todos).Error; err == nil {
			for i := range todos {
				cal.Children = append(cal.Children, todoComponent(&todos[i]))
			}
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
