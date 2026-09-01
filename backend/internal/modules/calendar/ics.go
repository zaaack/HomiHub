package modulecalendar

import (
	"bytes"
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
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//HomiHub//Team Calendar//CN")
	cal.Props.SetText("X-WR-CALNAME", name)
	cal.Props.SetText("X-WR-CALDESC", name)
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
