package modulecalendar

import (
	"bytes"
	"errors"
	"fmt"
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
	Class        string
	Duration     string
	StartsAt     time.Time
	EndsAt       time.Time
	AllDay       bool
	RRule        string
	ExDates      []time.Time
	RecurrenceID *time.Time
	RelatedTo    string
	Reminders    []models.Reminder
}

// exDatesJoin renders exception dates as a comma-separated RFC3339 string for
// the model's ExDate column.
func exDatesJoin(dates []time.Time) string {
	return models.ExDateJoin(dates)
}

// exDatesSplit parses the model's ExDate column back into UTC times.
func exDatesSplit(s string) []time.Time {
	return models.ExDateSplit(s)
}

// writeExDates emits one EXDATE prop per exception date (go-ical parses a
// single EXDATE prop as a single value, so splitting them round-trips safely).
func writeExDates(comp *ical.Component, dates []time.Time) {
	for _, d := range dates {
		p := ical.NewProp(ical.PropExceptionDates)
		p.SetDateTime(d.UTC())
		comp.Props.Set(p)
	}
}

// parseAlarm decodes a single VALARM TRIGGER into a Reminder, supporting both
// relative durations ("-PT15M") and absolute date-times (TRIGGER;VALUE=DATE-TIME).
func parseAlarm(tr *ical.Prop) (models.Reminder, bool) {
	if tr.ValueType() == ical.ValueDateTime || tr.ValueType() == ical.ValueDate {
		t, err := tr.DateTime(nil)
		if err != nil {
			return models.Reminder{}, false
		}
		u := t.UTC()
		return models.Reminder{Unit: "at", At: &u}, true
	}
	secs := parseTrigger(tr.Value)
	if secs >= 0 {
		return models.Reminder{}, false
	}
	secs = -secs
	switch {
	case secs%86400 == 0:
		return models.Reminder{Unit: "day", Value: secs / 86400}, true
	case secs%3600 == 0:
		return models.Reminder{Unit: "hour", Value: secs / 3600}, true
	default:
		return models.Reminder{Unit: "min", Value: secs / 60}, true
	}
}

func BuildCalendar(name string, evs []models.CalendarEvent) *ical.Calendar {
	cal := newCalendar(name)
	for i := range evs {
		ev := &evs[i]
		comp := ical.NewEvent()
		comp.Props.SetText(ical.PropUID, ev.UID)
		comp.Props.SetText(ical.PropSummary, ev.Title)
		comp.Props.SetDateTime(ical.PropDateTimeStamp, ev.UpdatedAt.UTC())
		if ev.AllDay {
			comp.Props.SetDate(ical.PropDateTimeStart, ev.StartsAt)
			comp.Props.SetDate(ical.PropDateTimeEnd, ev.EndsAt)
		} else {
			comp.Props.SetDateTime(ical.PropDateTimeStart, ev.StartsAt)
			if ev.Duration != "" {
				comp.Props.SetText(ical.PropDuration, ev.Duration)
			} else {
				comp.Props.SetDateTime(ical.PropDateTimeEnd, ev.EndsAt)
			}
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
		if ev.Class != "" {
			comp.Props.SetText(ical.PropClass, ev.Class)
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
		for _, rel := range strings.Split(ev.RelatedTo, ",") {
			if rel == "" {
				continue
			}
			p := ical.NewProp(ical.PropRelatedTo)
			p.Value = strings.TrimSpace(rel)
			comp.Props.Add(p)
		}
		if rem := parseRemindersJSON(ev.Reminders); len(rem) > 0 {
			addAlarms(comp.Component, ev.StartsAt, rem)
		}
		writeExDates(comp.Component, exDatesSplit(ev.ExDate))
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
	UID          string
	Title        string
	Note         string
	Location     string
	URL          string
	StartsAt     *time.Time
	DueAt        *time.Time
	Completed    bool
	Percent      int
	Priority     int
	RRule        string
	ExDates      []time.Time
	Group        string
	Tags         []string
	ParentUID    string
	Reminders    []models.Reminder
	CompletedAt  *time.Time
}

var errNoTodo = errors.New("no VTODO in calendar")

func parseRemindersJSON(s string) []models.Reminder {
	return models.ParseReminders(s)
}

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
		pt.Location, _ = child.Props.Text(ical.PropLocation)
		pt.URL, _ = child.Props.Text(ical.PropURL)
		if dt := child.Props.Get(ical.PropDateTimeStart); dt != nil {
			t, err := dt.DateTime(nil)
			if err == nil {
				pt.StartsAt = &t
			}
		}
		if due := child.Props.Get(ical.PropDue); due != nil {
			t, err := due.DateTime(nil)
			if err != nil {
				return nil, err
			}
			pt.DueAt = &t
		} else if d := child.Props.Get(ical.PropDuration); d != nil && pt.StartsAt != nil {
			// No DUE but a DURATION: fold it into DueAt so both storage and
			// RFC4791 §9.9 time-range matching treat the interval as ended.
			dur, err := d.Duration()
			if err != nil {
				return nil, err
			}
			t := pt.StartsAt.Add(dur)
			pt.DueAt = &t
		}
		status, _ := child.Props.Text(ical.PropStatus)
		if strings.EqualFold(status, "COMPLETED") {
			pt.Completed = true
		} else if strings.EqualFold(status, "IN-PROCESS") {
			pt.Percent = 50
		}
		if p := child.Props.Get(ical.PropPercentComplete); p != nil {
			fmt.Sscanf(p.Value, "%d", &pt.Percent)
		}
		if p := child.Props.Get(ical.PropPriority); p != nil {
			fmt.Sscanf(p.Value, "%d", &pt.Priority)
		}
		if rp := child.Props.Get(ical.PropRecurrenceRule); rp != nil {
			pt.RRule = rp.Value
		}
		var cats []string
		for _, p := range child.Props[ical.PropCategories] {
			cats = append(cats, p.Value)
		}
		cat := strings.Join(cats, ",")
		cat = strings.ReplaceAll(cat, `\,`, ",")
		parts := strings.Split(cat, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		parts = filterEmpty(parts)
		if len(parts) > 0 {
			pt.Group = parts[0]
		}
		if len(parts) > 1 {
			pt.Tags = parts[1:]
		}
		if rel := child.Props.Get(ical.PropRelatedTo); rel != nil {
			pt.ParentUID = rel.Value
		}
		if rt, err := child.Props.Text(ical.PropCompleted); err == nil {
			if t, e := time.Parse(time.RFC3339, rt); e == nil {
				pt.CompletedAt = &t
			}
		}
		for _, ex := range child.Props[ical.PropExceptionDates] {
			if t, err := ex.DateTime(nil); err == nil {
				pt.ExDates = append(pt.ExDates, t.UTC())
			}
		}
		for _, a := range child.Children {
			if a.Name != ical.CompAlarm {
				continue
			}
			tr := a.Props.Get(ical.PropTrigger)
			if tr == nil {
				continue
			}
			if r, ok := parseAlarm(tr); ok {
				pt.Reminders = append(pt.Reminders, r)
			}
		}
		return pt, nil
	}
	return nil, errNoTodo
}

// parseTrigger parses an iCal TRIGGER duration like "-PT15M" / "-PT1H" / "-P1D"
// and returns the total seconds (negative = before the event).
func parseTrigger(v string) int {
	v = strings.TrimSpace(v)
	neg := false
	if strings.HasPrefix(v, "-") {
		neg = true
		v = v[1:]
	}
	if strings.HasPrefix(v, "+") {
		v = v[1:]
	}
	v = strings.TrimPrefix(v, "P")
	if v == "" {
		return 0
	}
	secs := 0
	num := ""
	mult := 1
	for _, ch := range v {
		switch {
		case ch >= '0' && ch <= '9':
			num += string(ch)
		case ch == 'T':
		case ch == 'W':
			secs += atoi(num) * 604800
			num = ""
		case ch == 'D':
			secs += atoi(num) * 86400
			num = ""
		case ch == 'H':
			secs += atoi(num) * 3600
			num = ""
		case ch == 'M':
			secs += atoi(num) * 60
			num = ""
		case ch == 'S':
			secs += atoi(num)
			num = ""
		default:
			_ = mult
		}
	}
	if neg {
		return -secs
	}
	return secs
}

func atoi(s string) int {
	n := 0
	for _, ch := range s {
		n = n*10 + int(ch-'0')
	}
	return n
}

// addAlarms appends VALARM components to a component. Relative reminders are
// rendered as negative durations relative to base; absolute ("at") reminders as
// TRIGGER;VALUE=DATE-TIME.
func addAlarms(comp *ical.Component, base time.Time, reminders []models.Reminder) {
	for _, r := range reminders {
		al := ical.NewComponent(ical.CompAlarm)
		al.Props.SetText(ical.PropAction, "DISPLAY")
		al.Props.SetText(ical.PropDescription, "Reminder")
		t := ical.NewProp(ical.PropTrigger)
		if r.Unit == "at" && r.At != nil {
			t.SetDateTime(r.At.UTC())
		} else {
			if r.Value <= 0 {
				continue
			}
			t.Value = formatTrigger(-r.Seconds())
		}
		al.Props.Set(t)
		comp.Children = append(comp.Children, al)
	}
}

// formatTrigger renders a duration in seconds as an iCal duration string.
func formatTrigger(secs int) string {
	neg := ""
	if secs < 0 {
		neg = "-"
		secs = -secs
	}
	d := secs / 86400
	secs %= 86400
	h := secs / 3600
	secs %= 3600
	m := secs / 60
	s := secs % 60
	out := "P"
	if d > 0 {
		out += fmt.Sprintf("%dD", d)
	}
	if h > 0 || m > 0 || s > 0 {
		out += "T"
		if h > 0 {
			out += fmt.Sprintf("%dH", h)
		}
		if m > 0 {
			out += fmt.Sprintf("%dM", m)
		}
		if s > 0 {
			out += fmt.Sprintf("%dS", s)
		}
	}
	return neg + out
}

func tagsToCSV(tags []string) string {
	return strings.Join(tags, ",")
}

func filterEmpty(s []string) []string {
	out := s[:0]
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func serialize(cal *ical.Calendar) []byte {
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err == nil {
		return buf.Bytes()
	}
	name, _ := cal.Props.Text("X-WR-CALNAME")
	if name == "" {
		name, _ = cal.Props.Text("X-WR-CALDESC")
	}
	return []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//HomiHub//Team Calendar//CN\r\nX-WR-CALNAME:" + name + "\r\nEND:VCALENDAR\r\n")
}

func ParseEvents(cal *ical.Calendar) []parsedEvent {
	out := []parsedEvent{}
	for _, ev := range cal.Events() {
		pe := parsedEvent{}
		pe.UID, _ = ev.Props.Text(ical.PropUID)
		if pe.UID == "" {
			continue
		}
		pe.Title, _ = ev.Props.Text(ical.PropSummary)
		pe.Location, _ = ev.Props.Text(ical.PropLocation)
		pe.Description, _ = ev.Props.Text(ical.PropDescription)
		if cat, err := ev.Props.Text(ical.PropCategories); err == nil {
			pe.Category = cat
		}
		if cls, err := ev.Props.Text(ical.PropClass); err == nil {
			pe.Class = cls
		}
		if dur := ev.Props.Get(ical.PropDuration); dur != nil {
			pe.Duration = dur.Value
		}
		start, err := ev.DateTimeStart(nil)
		if err != nil {
			continue
		}
		end, err := ev.DateTimeEnd(nil)
		if err != nil {
			continue
		}
		pe.StartsAt, pe.EndsAt = start.UTC(), end.UTC()
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
		for _, rel := range ev.Props[ical.PropRelatedTo] {
			if pe.RelatedTo != "" {
				pe.RelatedTo += ","
			}
			pe.RelatedTo += rel.Value
		}
		for _, ex := range ev.Props[ical.PropExceptionDates] {
			if t, err := ex.DateTime(nil); err == nil {
				pe.ExDates = append(pe.ExDates, t.UTC())
			}
		}
		for _, a := range ev.Children {
			if a.Name != ical.CompAlarm {
				continue
			}
			tr := a.Props.Get(ical.PropTrigger)
			if tr == nil {
				continue
			}
			if r, ok := parseAlarm(tr); ok {
				pe.Reminders = append(pe.Reminders, r)
			}
		}
		out = append(out, pe)
	}
	return out
}
