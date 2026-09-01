package modulecalendar

import (
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"homihub/backend/internal/models"
)

func TestReminderSeconds(t *testing.T) {
	cases := []struct {
		r    models.Reminder
		want int
	}{
		{models.Reminder{Unit: "min", Value: 15}, 900},
		{models.Reminder{Unit: "hour", Value: 1}, 3600},
		{models.Reminder{Unit: "day", Value: 2}, 172800},
		{models.Reminder{Unit: "foo", Value: 3}, 180},
	}
	for _, c := range cases {
		if got := c.r.Seconds(); got != c.want {
			t.Errorf("Reminder{unit=%q,value=%d}.Seconds() = %d, want %d", c.r.Unit, c.r.Value, got, c.want)
		}
	}
}

func TestParseTrigger(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"-PT15M", -900},
		{"-PT1H", -3600},
		{"-P1D", -86400},
		{"-PT1H30M", -5400},
		{"-P2DT2H", -180000},
		{"PT10M", 600},
		{"P1W", 604800},
		{"", 0},
	}
	for _, c := range cases {
		if got := parseTrigger(c.in); got != c.want {
			t.Errorf("parseTrigger(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFormatTriggerRoundTrip(t *testing.T) {
	for _, secs := range []int{-900, -3600, -86400, -5400, -180000, 600} {
		formatted := formatTrigger(secs)
		if got := parseTrigger(formatted); got != secs {
			t.Errorf("roundtrip %d -> %q -> %d", secs, formatted, got)
		}
	}
}

func TestTodoComponentFullFields(t *testing.T) {
	now := time.Now().UTC()
	due := now.Add(24 * time.Hour)
	start := now.Add(2 * time.Hour)
	completedAt := now
	todo := &models.Todo{
		UID:         "uid-abc",
		Title:       "买牛奶",
		Note:        "记得拿优惠券",
		Location:    "超市",
		URL:         "https://example.com",
		StartAt:     &start,
		DueAt:       &due,
		Completed:   false,
		Percent:     50,
		Priority:    3,
		RRule:       "FREQ=WEEKLY;BYDAY=MO,WE,FR",
		Group:       "购物",
		Tags:        "生鲜,促销",
		ParentID:    "todo-parent",
		Reminders:   `[{"unit":"min","value":15},{"unit":"hour","value":1}]`,
		CompletedAt: &completedAt,
		UpdatedAt:   now,
	}
	comp := todoComponent(todo)
	uid, _ := comp.Props.Text(ical.PropUID)
	if uid != "uid-abc" {
		t.Errorf("UID = %q", uid)
	}
	summary, _ := comp.Props.Text(ical.PropSummary)
	if summary != "买牛奶" {
		t.Errorf("SUMMARY = %q", summary)
	}
	loc, _ := comp.Props.Text(ical.PropLocation)
	if loc != "超市" {
		t.Errorf("LOCATION = %q", loc)
	}
	cat, _ := comp.Props.Text(ical.PropCategories)
	if cat != "购物,生鲜,促销" {
		t.Errorf("CATEGORIES = %q", cat)
	}
	rel, _ := comp.Props.Text(ical.PropRelatedTo)
	if rel != "todo-parent" {
		t.Errorf("RELATED-TO = %q", rel)
	}
	status, _ := comp.Props.Text(ical.PropStatus)
	if status != "IN-PROCESS" {
		t.Errorf("STATUS = %q", status)
	}
	if p := comp.Props.Get(ical.PropPriority); p == nil || p.Value != "3" {
		t.Errorf("PRIORITY = %v", p)
	}
	alarmCount := 0
	for _, ch := range comp.Children {
		if ch.Name == ical.CompAlarm {
			alarmCount++
		}
	}
	if alarmCount != 2 {
		t.Errorf("VALARM count = %d, want 2", alarmCount)
	}
}

func TestParseTodoFullFields(t *testing.T) {
	cal := ical.NewCalendar()
	comp := ical.NewComponent(ical.CompToDo)
	comp.Props.SetText(ical.PropUID, "uid-xyz")
	comp.Props.SetText(ical.PropSummary, "汇报")
	comp.Props.SetText(ical.PropDescription, "周报")
	comp.Props.SetText(ical.PropLocation, "会议室")
	comp.Props.SetText(ical.PropURL, "https://docs.example.com")
	comp.Props.SetText(ical.PropCategories, "工作,文档")
	comp.Props.SetText(ical.PropRelatedTo, "uid-parent")
	comp.Props.SetText(ical.PropStatus, "COMPLETED")
	comp.Props.SetText(ical.PropPercentComplete, "100")
	comp.Props.SetText(ical.PropPriority, "9")
	comp.Props.SetText(ical.PropRecurrenceRule, "FREQ=MONTHLY;BYMONTHDAY=1,15")
	al := ical.NewComponent(ical.CompAlarm)
	al.Props.SetText(ical.PropAction, "DISPLAY")
	tr := ical.NewProp(ical.PropTrigger); tr.Value = "-PT30M"; al.Props.Set(tr)
	comp.Children = append(comp.Children, al)
	cal.Children = append(cal.Children, comp)

	pt, err := ParseTodo(cal)
	if err != nil {
		t.Fatalf("ParseTodo: %v", err)
	}
	if pt.UID != "uid-xyz" || pt.Title != "汇报" || pt.Note != "周报" {
		t.Errorf("basic fields: %+v", pt)
	}
	if pt.Location != "会议室" || pt.URL != "https://docs.example.com" {
		t.Errorf("loc/url: %+v", pt)
	}
	if pt.Group != "工作" || len(pt.Tags) != 1 || pt.Tags[0] != "文档" {
		t.Errorf("group/tags: %+v", pt)
	}
	if pt.ParentUID != "uid-parent" {
		t.Errorf("parent: %q", pt.ParentUID)
	}
	if !pt.Completed || pt.Percent != 100 || pt.Priority != 9 {
		t.Errorf("status: %+v", pt)
	}
	if !strings.Contains(pt.RRule, "MONTHLY") || !strings.Contains(pt.RRule, "BYMONTHDAY") {
		t.Errorf("rrule: %q", pt.RRule)
	}
	if len(pt.Reminders) != 1 || pt.Reminders[0] != (models.Reminder{Unit: "min", Value: 30}) {
		t.Errorf("reminders: %+v", pt.Reminders)
	}
}

func TestParseTodoSubMinuteAndDayUnits(t *testing.T) {
	cal := ical.NewCalendar()
	comp := ical.NewComponent(ical.CompToDo)
	comp.Props.SetText(ical.PropUID, "u1")
	comp.Props.SetText(ical.PropSummary, "t")
	for _, trig := range []string{"-PT5M", "-PT2H", "-P3D"} {
		al := ical.NewComponent(ical.CompAlarm)
		tr := ical.NewProp(ical.PropTrigger); tr.Value = trig; al.Props.Set(tr)
		comp.Children = append(comp.Children, al)
	}
	cal.Children = append(cal.Children, comp)
	pt, err := ParseTodo(cal)
	if err != nil {
		t.Fatal(err)
	}
	want := []models.Reminder{
		{Unit: "min", Value: 5},
		{Unit: "hour", Value: 2},
		{Unit: "day", Value: 3},
	}
	if len(pt.Reminders) != len(want) {
		t.Fatalf("reminders = %+v", pt.Reminders)
	}
	for i := range want {
		if pt.Reminders[i] != want[i] {
			t.Errorf("reminders[%d] = %+v, want %+v", i, pt.Reminders[i], want[i])
		}
	}
}

func TestParseRuleAdvanced(t *testing.T) {
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		rrule string
		want  []string
	}{
		{"FREQ=DAILY;INTERVAL=3", []string{"2026-09-01", "2026-09-04", "2026-09-07"}},
		{"FREQ=WEEKLY;BYDAY=MO,WE,FR", []string{"2026-09-02", "2026-09-04", "2026-09-07"}},
		{"FREQ=MONTHLY;BYMONTHDAY=1,15", []string{"2026-09-01", "2026-09-15", "2026-10-01"}},
	}
	for _, c := range cases {
		rr, err := parseRuleWithStart(c.rrule, start)
		if err != nil {
			t.Fatalf("%s: %v", c.rrule, err)
		}
		occ := rr.Between(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), true)
		got := make([]string, 0, len(occ))
		for _, o := range occ {
			got = append(got, o.Format("2006-01-02"))
		}
		for i, w := range c.want {
			if i >= len(got) || got[i] != w {
				t.Errorf("%s: occ[%d] = %v, want %q (all: %v)", c.rrule, i, got[i], w, got)
				break
			}
		}
	}
}

func TestTodoComponentCompletedFields(t *testing.T) {
	now := time.Now().UTC()
	todo := &models.Todo{
		UID:         "u-done",
		Title:       "已完成",
		Completed:   true,
		Percent:     100,
		CompletedAt: &now,
		UpdatedAt:   now,
	}
	comp := todoComponent(todo)
	status, _ := comp.Props.Text(ical.PropStatus)
	if status != "COMPLETED" {
		t.Errorf("STATUS = %q", status)
	}
	pc, _ := comp.Props.Text(ical.PropPercentComplete)
	if pc != "100" {
		t.Errorf("PERCENT-COMPLETE = %q", pc)
	}
}
