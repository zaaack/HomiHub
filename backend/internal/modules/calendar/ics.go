package modulecalendar

import (
	"bytes"
	"errors"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"homihub/backend/internal/models"
)

type parsedEvent struct {
	UID          string
	Title        string
	Location     string
	Description  string
	Category     string
	StartsAt     time.Time
	EndsAt       time.Time
	AllDay       bool
	RRule        string
	RecurrenceID *time.Time
}

func BuildCalendar(name string, evs []models.CalendarEvent) *ical.Calendar {
	cal := newCalendar(name)
	for i := range evs {
		ev := &evs[i]
		comp := ical.NewEvent()
		comp.Props.SetText(ical.PropUID, ev.UID)
		comp.Props.SetText(ical.PropSummary, ev.Title)
		comp.Props.SetDateTime(ical.PropDateTimeStamp, ev.UpdatedAt)
		if ev.AllDay {
			comp.Props.SetDate(ical.PropDateTimeStart, ev.StartsAt)
			comp.Props.SetDate(ical.PropDateTimeEnd, ev.EndsAt)
		} else {
			comp.Props.SetDateTime(ical.PropDateTimeStart, ev.StartsAt)
			comp.Props.SetDateTime(ical.PropDateTimeEnd, ev.EndsAt)
		}
		if ev.Location != "" {
			comp.Props.SetText(ical.PropLocation, ev.Location)
		}
		if ev.Description != "" {
			comp.Props.SetText(ical.PropDescription, ev.Description)
		}
		if ev.Category != "" {
			comp.Props.SetText(ical.PropCategories, ev.Category)
		}
		if ev.RRule != "" {
			p := ical.NewProp(ical.PropRecurrenceRule)
			p.Value = ev.RRule
			comp.Props.Set(p)
		}
		if ev.RecurrenceID != nil {
			rid := ical.NewProp(ical.PropRecurrenceID)
			rid.SetDateTime(*ev.RecurrenceID)
			comp.Props.Set(rid)
		}
		cal.Children = append(cal.Children, comp.Component)
	}
	return cal
}

// BuildTodoCalendar renders a single todo as a VTODO component inside a
// calendar, so CalDAV clients (tasks.org, Apple Reminders, etc.) can read it.
func BuildTodoCalendar(name string, t *models.Todo) *ical.Calendar {
	cal := newCalendar(name)
	cal.Children = append(cal.Children, todoComponent(t))
	return cal
}

func newCalendar(name string) *ical.Calendar {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//HomiHub//Team Calendar//CN")
	cal.Props.SetText("X-WR-CALNAME", name)
	cal.Props.SetText("X-WR-CALDESC", name)
	return cal
}

// todoUID returns the stable UID for a todo, falling back to the legacy
// derived form for rows created before the UID column existed.
func todoUID(t *models.Todo) string {
	if t.UID != "" {
		return t.UID
	}
	return "todo-" + t.ID
}

// parsedTodo is a VTODO component decoded from an incoming iCalendar payload.
type parsedTodo struct {
	UID       string
	Title     string
	Note      string
	DueAt     *time.Time
	Completed bool
	RRule     string
}

var errNoTodo = errors.New("no VTODO in calendar")

// ParseTodo extracts the first VTODO component from a calendar.
func ParseTodo(cal *ical.Calendar) (*parsedTodo, error) {
	for _, child := range cal.Children {
		if child.Name != ical.CompToDo {
			continue
		}
		pt := &parsedTodo{}
		pt.UID, _ = child.Props.Text(ical.PropUID)
		pt.Title, _ = child.Props.Text(ical.PropSummary)
		pt.Note, _ = child.Props.Text(ical.PropDescription)
		if due := child.Props.Get(ical.PropDue); due != nil {
			t, err := due.DateTime(nil)
			if err != nil {
				return nil, err
			}
			pt.DueAt = &t
		}
		status, _ := child.Props.Text(ical.PropStatus)
		if strings.EqualFold(status, "COMPLETED") {
			pt.Completed = true
		}
		if rp := child.Props.Get(ical.PropRecurrenceRule); rp != nil {
			pt.RRule = rp.Value
		}
		return pt, nil
	}
	return nil, errNoTodo
}

func serialize(cal *ical.Calendar) []byte {
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return nil
	}
	return buf.Bytes()
}

func ParseEvents(cal *ical.Calendar) []parsedEvent {
	out := []parsedEvent{}
	for _, ev := range cal.Events() {
		pe := parsedEvent{Category: "family"}
		pe.UID, _ = ev.Props.Text(ical.PropUID)
		if pe.UID == "" {
			continue
		}
		pe.Title, _ = ev.Props.Text(ical.PropSummary)
		pe.Location, _ = ev.Props.Text(ical.PropLocation)
		pe.Description, _ = ev.Props.Text(ical.PropDescription)
		if cat, err := ev.Props.Text(ical.PropCategories); err == nil {
			if _, ok := Categories[cat]; ok {
				pe.Category = cat
			}
		}
		start, err := ev.DateTimeStart(nil)
		if err != nil {
			continue
		}
		end, err := ev.DateTimeEnd(nil)
		if err != nil {
			continue
		}
		pe.StartsAt, pe.EndsAt = start, end
		if sp := ev.Props.Get(ical.PropDateTimeStart); sp != nil {
			pe.AllDay = sp.ValueType() == ical.ValueDate
		}
		if rp := ev.Props.Get(ical.PropRecurrenceRule); rp != nil {
			pe.RRule = rp.Value
		}
		if rp := ev.Props.Get(ical.PropRecurrenceID); rp != nil {
			t, err := rp.DateTime(nil)
			if err != nil {
				continue
			}
			rid := t.UTC()
			pe.RecurrenceID = &rid
		}
		out = append(out, pe)
	}
	return out
}
