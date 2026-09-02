package modulecalendar

import (
	"strings"

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

func matchCIPropFilter(filter caldav.PropFilter, comp *ical.Component) (bool, error) {
	field := comp.Props.Get(filter.Name)
	if field == nil {
		return filter.IsNotDefined, nil
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
