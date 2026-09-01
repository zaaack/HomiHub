package modulecalendar

import (
	"time"

	"github.com/teambition/rrule-go"
)

func parseRule(s string) (*rrule.RRule, error) {
	return rrule.StrToRRule(s)
}

func parseRuleWithStart(s string, dtstart time.Time) (*rrule.RRule, error) {
	opt, err := rrule.StrToROption(s)
	if err != nil {
		return nil, err
	}
	opt.Dtstart = dtstart
	return rrule.NewRRule(*opt)
}
