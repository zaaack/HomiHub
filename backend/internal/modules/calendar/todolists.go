package modulecalendar

import (
	"strings"

	"github.com/emersion/go-ical"
	"gorm.io/gorm"

	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
)

// IsPureTodoList reports whether a custom calendar row is a VTODO-only todo
// list, as opposed to a general calendar that also accepts VEVENT/VJOURNAL.
func IsPureTodoList(c *models.Calendar) bool {
	comps := []string{}
	for _, p := range strings.Split(c.Components, ",") {
		if p = strings.TrimSpace(strings.ToUpper(p)); p != "" {
			comps = append(comps, p)
		}
	}
	return len(comps) == 1 && comps[0] == ical.CompToDo
}

func calRow(db *gorm.DB, teamID, name string) (*models.Calendar, error) {
	var c models.Calendar
	err := middleware.ScopedDB(db, teamID).Where("name = ?", name).First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func isSharedCal(db *gorm.DB, teamID, name, userID string) bool {
	if userID == "" {
		return false
	}
	var n int64
	middleware.ScopedDB(db, teamID).Model(&models.CalendarShare{}).
		Where("calendar = ? AND user_id = ?", name, userID).Count(&n)
	return n > 0
}

// TodoListRole reports the requesting user's access to a custom calendar name:
//
//   - "owner" / "member" for member-scoped todo lists (created in the web UI),
//   - "team" for any other custom calendar (visible to the whole team),
//   - "" when the calendar does not exist or the user is not allowed.
//
// Built-in "self"/"team" calendars are handled by callers and never passed here.
func TodoListRole(db *gorm.DB, teamID, userID, name string) string {
	if name == "" || userID == "" {
		return ""
	}
	c, err := calRow(db, teamID, name)
	if err != nil {
		return ""
	}
	if c.Access != models.CalendarAccessMembers {
		return "team"
	}
	if c.OwnerID != "" && c.OwnerID == userID {
		return "owner"
	}
	if isSharedCal(db, teamID, name, userID) {
		return "member"
	}
	return ""
}

// TodoListsForUser returns every custom calendar in the team that may hold
// todos the user is allowed to read: general custom calendars (legacy whole
// team access) plus member-scoped VTODO lists the user owns or is shared with.
func TodoListsForUser(db *gorm.DB, teamID, userID string) []models.Calendar {
	var cals []models.Calendar
	if err := middleware.ScopedDB(db, teamID).Order("created_at").Find(&cals).Error; err != nil {
		return nil
	}
	out := make([]models.Calendar, 0, len(cals))
	for i := range cals {
		c := &cals[i]
		if !strings.Contains(strings.ToUpper(c.Components), ical.CompToDo) {
			continue // no VTODO component: no todos can live here
		}
		if c.Access == models.CalendarAccessMembers && c.OwnerID != userID &&
			!isSharedCal(db, teamID, c.Name, userID) {
			continue
		}
		out = append(out, *c)
	}
	return out
}
