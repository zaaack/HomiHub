package modulecalendar

import (
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
)

// filterCalendarObjects mirrors caldav.Filter but strips time-range first
// (the DB layer already handles it, and caldav.Filter's matchCompTimeRange
// drops VTODO). Text-match honors the collation attribute (i;octet vs
// i;ascii-casemap) — see matchCITextMatch.
func filterCalendarObjects(query *caldav.CalendarQuery, objs []caldav.CalendarObject) ([]caldav.CalendarObject, error) {
	if query == nil {
		return objs, nil
	}
	var out []caldav.CalendarObject
	for i := range objs {
		co := &objs[i]
		if co.Data == nil || co.Data.Component == nil {
			continue
		}
		ok, err := matchCIFilter(query.CompFilter, co.Data.Component)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, objs[i])
		}
	}
	return out, nil
}

// matchCIFilter mirrors caldav.match with case-insensitive text matching.
func matchCIFilter(filter caldav.CompFilter, comp *ical.Component) (bool, error) {
	if comp.Name != filter.Name {
		return filter.IsNotDefined, nil
	}
	for _, compFilter := range filter.Comps {
		match, err := matchCICompFilter(compFilter, comp)
		if err != nil {
			return false, err
		}
		if !match {
			return false, nil
		}
	}
	for _, propFilter := range filter.Props {
		match, err := matchCIPropFilter(propFilter, comp)
		if err != nil {
			return false, err
		}
		if !match {
			return false, nil
		}
	}
	return true, nil
}

func matchCICompFilter(filter caldav.CompFilter, comp *ical.Component) (bool, error) {
	// A VALARM comp-filter with a time-range bounds the alarm's trigger time
	// (RFC 4791 §9.9); the trigger is relative to the parent event's DTSTART.
	if filter.Name == ical.CompAlarm && (!filter.Start.IsZero() || !filter.End.IsZero()) {
		return matchCIAlarmRange(filter, comp), nil
	}
	var matches []*ical.Component
	for _, child := range comp.Children {
		match, err := matchCIFilter(filter, child)
		if err != nil {
			return false, err
		} else if match {
			matches = append(matches, child)
		}
	}
	if len(matches) == 0 {
		return filter.IsNotDefined, nil
	}
	return true, nil
}

// matchCIAlarmRange reports whether any VALARM child of comp triggers within
// [filter.Start, filter.End).  Relative TRIGGERs are anchored to the parent
// event's DTSTART; absolute TRIGGERs are compared directly.
func matchCIAlarmRange(filter caldav.CompFilter, comp *ical.Component) bool {
	start := time.Time{}
	if sp := comp.Props.Get(ical.PropDateTimeStart); sp != nil {
		if t, err := sp.DateTime(nil); err == nil {
			start = t
		}
	}
	for _, child := range comp.Children {
		if child.Name != ical.CompAlarm {
			continue
		}
		tr := child.Props.Get(ical.PropTrigger)
		if tr == nil {
			continue
		}
		var trigger time.Time
		switch tr.ValueType() {
		case ical.ValueDateTime, ical.ValueDate:
			if t, err := tr.DateTime(nil); err == nil {
				trigger = t
			}
		default:
			if !start.IsZero() {
				trigger = start.Add(time.Duration(parseTrigger(tr.Value)) * time.Second)
			}
		}
		if trigger.IsZero() {
			continue
		}
		if !trigger.Before(filter.Start) && (filter.End.IsZero() || trigger.Before(filter.End)) {
			return true
		}
	}
	return false
}

func matchCIPropFilter(filter caldav.PropFilter, comp *ical.Component) (bool, error) {
	field := comp.Props.Get(filter.Name)
	if field == nil {
		return filter.IsNotDefined, nil
	}
	if filter.IsNotDefined {
		return false, nil
	}
	for _, paramFilter := range filter.ParamFilter {
		if !matchCIParamFilter(paramFilter, field) {
			return false, nil
		}
	}
	if filter.TextMatch != nil {
		return matchCITextMatch(*filter.TextMatch, field.Value), nil
	}
	return true, nil
}

func matchCIParamFilter(filter caldav.ParamFilter, field *ical.Prop) bool {
	value := field.Params.Get(filter.Name)
	if value == "" {
		return filter.IsNotDefined
	} else if filter.IsNotDefined {
		return false
	}
	if filter.TextMatch != nil {
		return matchCITextMatch(*filter.TextMatch, value)
	}
	return true
}

// matchCITextMatch honors the collation attribute (RFC 4791 §9.7.5):
// "i;octet" (default) is case-sensitive, "i;ascii-casemap" is
// case-insensitive. The fork of go-webdav preserves Collation while decoding.
func matchCITextMatch(txt caldav.TextMatch, value string) bool {
	query := txt.Text
	if txt.Collation == "i;ascii-casemap" {
		query = strings.ToLower(query)
		value = strings.ToLower(value)
	}
	match := strings.Contains(value, query)
	if txt.NegateCondition {
		match = !match
	}
	return match
}
