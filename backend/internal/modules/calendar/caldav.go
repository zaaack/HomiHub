package modulecalendar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"runtime/debug"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	"homihub/backend/internal/modules"
)

const (
	calSelf = "self"
	calTeam = "team"
)

var errNotFound = fs.ErrNotExist

func calendarPath(email string, cal string) string {
	return "/dav/" + url.PathEscape(email) + "/calendars/" + cal + "/"
}

func principalPath(email string) string {
	return "/dav/" + url.PathEscape(email) + "/"
}

func calNameFromPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) >= 4 && parts[0] == "dav" && parts[2] == "calendars" {
		return parts[3]
	}
	return ""
}

type DavSession struct {
	Team  *models.Team
	Email string
	User  *models.User
}

type davKey struct{}

type davBackend struct {
	app *modules.App
}

func (b *davBackend) session(ctx context.Context) *DavSession {
	s, _ := ctx.Value(davKey{}).(*DavSession)
	return s
}

func (b *davBackend) CurrentUserPrincipal(ctx context.Context) (string, error) {
	return principalPath(b.session(ctx).Email), nil
}

func (b *davBackend) CalendarHomeSetPath(ctx context.Context) (string, error) {
	return "/dav/" + url.PathEscape(b.session(ctx).Email) + "/calendars/", nil
}

func (b *davBackend) CreateCalendar(ctx context.Context, calendar *caldav.Calendar) error {
	s := b.session(ctx)
	name := calNameFromPath(calendar.Path)
	if name == "" {
		return errors.New("无效的日历路径")
	}
	if name == calSelf || name == calTeam {
		return errors.New("保留的日历名")
	}
	components := strings.Join(calendar.SupportedComponentSet, ",")
	if components == "" {
		components = strings.Join([]string{ical.CompEvent, ical.CompToDo, ical.CompJournal}, ",")
	}
	cal := models.Calendar{
		ID:          uuid.Must(uuid.NewV7()).String(),
		TeamID:      s.Team.ID,
		Name:        name,
		DisplayName: calendar.Name,
		Description: calendar.Description,
		Components:  components,
		OwnerID:     s.User.ID,
	}
	return middleware.ScopedDB(b.app.DB, s.Team.ID).Create(&cal).Error
}

// calendarComponents returns the supported component set of a calendar:
// built-in self/team calendars always accept VEVENT/VTODO/VJOURNAL; custom
// calendars return the set persisted at MKCALENDAR time (default all three).
func (b *davBackend) calendarComponents(ctx context.Context, cal string) ([]string, error) {
	if cal == calSelf || cal == calTeam {
		return []string{ical.CompEvent, ical.CompToDo, ical.CompJournal}, nil
	}
	var c models.Calendar
	if err := middleware.ScopedDB(b.app.DB, b.session(ctx).Team.ID).
		Where("name = ?", cal).First(&c).Error; err != nil {
		return nil, err
	}
	if c.Components == "" {
		return []string{ical.CompEvent, ical.CompToDo, ical.CompJournal}, nil
	}
	return strings.Split(c.Components, ","), nil
}

func (b *davBackend) supportsComponent(ctx context.Context, cal, comp string) bool {
	comps, err := b.calendarComponents(ctx, cal)
	if err != nil {
		return false
	}
	for _, c := range comps {
		if strings.EqualFold(c, comp) {
			return true
		}
	}
	return false
}

// componentOf reports the primary iCalendar component type of a payload.
func componentOf(cal *ical.Calendar) string {
	for _, child := range cal.Children {
		switch child.Name {
		case ical.CompToDo, ical.CompEvent, ical.CompJournal, ical.CompFreeBusy:
			return child.Name
		}
	}
	return ""
}

func (b *davBackend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
	s := b.session(ctx)
	allComps := []string{ical.CompEvent, ical.CompToDo, ical.CompJournal}
	// Read display name overrides for built-in calendars.
	sc := func() *gorm.DB { return middlewareScopedDB(b.app.DB, s.Team.ID) }
	overrides := map[string]models.Calendar{}
	for _, ov := range []string{calSelf, calTeam} {
		var c models.Calendar
		if err := sc().Where("name = ?", ov).First(&c).Error; err == nil {
			overrides[ov] = c
		}
	}
	selfName := "我的"
	selfDesc := "我的私人日历"
	if o, ok := overrides[calSelf]; ok {
		if o.DisplayName != "" {
			selfName = o.DisplayName
		}
		if o.Description != "" {
			selfDesc = o.Description
		}
	}
	teamName := s.Team.Name
	teamDesc := s.Team.Name + " 的共享日历"
	if o, ok := overrides[calTeam]; ok {
		if o.DisplayName != "" {
			teamName = o.DisplayName
		}
		if o.Description != "" {
			teamDesc = o.Description
		}
	}
	cals := []caldav.Calendar{
		{
			Path:                  calendarPath(s.Email, calSelf),
			Name:                  selfName,
			Description:           selfDesc,
			SupportedComponentSet: allComps,
		},
		{
			Path:                  calendarPath(s.Email, calTeam),
			Name:                  teamName,
			Description:           teamDesc,
			SupportedComponentSet: allComps,
		},
	}
	var dbCals []models.Calendar
	if err := sc().Order("created_at").Find(&dbCals).Error; err != nil {
		return nil, err
	}
	for _, c := range dbCals {
		if c.Name == calSelf || c.Name == calTeam {
			continue // already handled above
		}
		comps := strings.Split(c.Components, ",")
		if len(comps) == 0 || comps[0] == "" {
			comps = allComps
		}
		cals = append(cals, caldav.Calendar{
			Path:                  calendarPath(s.Email, c.Name),
			Name:                  c.DisplayName,
			Description:           c.Description,
			SupportedComponentSet: comps,
		})
	}
	return cals, nil
}

func (b *davBackend) GetCalendar(ctx context.Context, p string) (*caldav.Calendar, error) {
	s := b.session(ctx)
	cal := calNameFromPath(p)
	if cal == calSelf || cal == calTeam {
		name, desc := "我的", "我的私人日历"
		if cal == calTeam {
			name, desc = s.Team.Name, s.Team.Name+" 的共享日历"
		}
		var ov models.Calendar
		if err := middlewareScopedDB(b.app.DB, s.Team.ID).Where("name = ?", cal).First(&ov).Error; err == nil {
			if ov.DisplayName != "" {
				name = ov.DisplayName
			}
			if ov.Description != "" {
				desc = ov.Description
			}
		}
		return &caldav.Calendar{Path: p, Name: name, Description: desc, SupportedComponentSet: []string{ical.CompEvent, ical.CompToDo, ical.CompJournal}}, nil
	}
	var c models.Calendar
	if err := middleware.ScopedDB(b.app.DB, s.Team.ID).Where("name = ?", cal).First(&c).Error; err != nil {
		return nil, errNotFound
	}
	comps := strings.Split(c.Components, ",")
	if len(comps) == 0 || comps[0] == "" {
		comps = []string{ical.CompEvent, ical.CompToDo, ical.CompJournal}
	}
	return &caldav.Calendar{
		Path:                  p,
		Name:                  c.DisplayName,
		Description:           c.Description,
		SupportedComponentSet: comps,
	}, nil
}

func (b *davBackend) DeleteCalendar(ctx context.Context, p string) error {
	cal := calNameFromPath(p)
	if cal == calSelf || cal == calTeam {
		return caldav.NewHTTPError(http.StatusForbidden, "built-in calendar cannot be deleted")
	}
	err := middleware.ScopedDB(b.app.DB, b.session(ctx).Team.ID).Where("name = ?", cal).Delete(&models.Calendar{}).Error
	if err == nil {
		err = middleware.ScopedDB(b.app.DB, b.session(ctx).Team.ID).Where("calendar = ?", cal).Delete(&models.CalendarEvent{}).Error
	}
	return err
}

func (b *davBackend) SetCalendar(ctx context.Context, p string, cal *caldav.Calendar) error {
	s := b.session(ctx)
	calName := calNameFromPath(p)
	if calName == "" {
		return errors.New("无效的日历路径")
	}
	sc := func() *gorm.DB { return middlewareScopedDB(b.app.DB, s.Team.ID) }
	components := strings.Join(cal.SupportedComponentSet, ",")
	// For built-in calendars, save display name override.
	if calName == calSelf || calName == calTeam {
		var c models.Calendar
		if err := sc().Where("name = ?", calName).First(&c).Error; err != nil {
			// create override row
			c = models.Calendar{
				ID:      uuid.Must(uuid.NewV7()).String(),
				TeamID:  s.Team.ID,
				Name:    calName,
				OwnerID: s.User.ID,
			}
		}
		c.DisplayName = cal.Name
		c.Description = cal.Description
		if components != "" {
			c.Components = components
		}
		return sc().Save(&c).Error
	}
	// Custom calendar.
	var c models.Calendar
	if err := sc().Where("name = ?", calName).First(&c).Error; err != nil {
		return err
	}
	c.DisplayName = cal.Name
	c.Description = cal.Description
	if components != "" {
		c.Components = components
	}
	return sc().Save(&c).Error
}

// teamItems returns the resources visible in a calendar: VEVENT for both
// calendars, plus the VTODO in the matching calendar scope.
func (b *davBackend) teamItems(ctx context.Context, cal string) (evs []models.CalendarEvent, todos []models.Todo, err error) {
	s := b.session(ctx)
	// Each chain gets a fresh scoped session: GORM shares the underlying
	// statement (clone==0) and reusing one handle across chains merges WHERE
	// conditions, which corrupts subsequent queries.
	sc := func() *gorm.DB { return middlewareScopedDB(b.app.DB, s.Team.ID) }
	switch {
	case cal == calSelf:
		if err = sc().
			Where("user_id = ? AND visibility = ?", s.User.ID, models.VisibilityPrivate).
			Order("starts_at").Find(&evs).Error; err != nil {
			return nil, nil, err
		}
		if err = sc().
			Where("user_id = ? AND calendar = ?", s.User.ID, calSelf).
			Order("created_at").Find(&todos).Error; err != nil {
			return nil, nil, err
		}
	case cal == calTeam:
		if err = sc().
			Where("calendar = ?", calTeam).
			Order("starts_at").Find(&evs).Error; err != nil {
			return nil, nil, err
		}
		if err = sc().
			Where("calendar = ?", calTeam).
			Order("created_at").Find(&todos).Error; err != nil {
			return nil, nil, err
		}
	default:
		if err = sc().
			Where("calendar = ?", cal).
			Order("starts_at").Find(&evs).Error; err != nil {
			return nil, nil, err
		}
		if err = sc().
			Where("calendar = ?", cal).
			Order("created_at").Find(&todos).Error; err != nil {
			return nil, nil, err
		}
	}
	return evs, todos, nil
}

func eventUID(p string) string {
	base := path.Base(p)
	return strings.TrimSuffix(base, ".ics")
}

func eventEtag(ev *models.CalendarEvent) string {
	sum := sha256.Sum256([]byte(ev.UID + ev.StartsAt.String() + ev.RRule + ev.Title + ev.Location + ev.Reminders + ev.UpdatedAt.String()))
	return hex.EncodeToString(sum[:8])
}

// bundleEtag hashes the master event plus every exception so the resource ETag
// changes whenever any instance of the recurring series is modified.
func bundleEtag(evs []models.CalendarEvent) string {
	h := sha256.New()
	for i := range evs {
		ev := &evs[i]
		rid := ""
		if ev.RecurrenceID != nil {
			rid = ev.RecurrenceID.String()
		}
		fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|%s|%s|", ev.UID, rid, ev.StartsAt.String(),
			ev.RRule, ev.Title, ev.Location, ev.Reminders, ev.UpdatedAt.String())
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

func todoEtag(t *models.Todo) string {
	sum := sha256.Sum256([]byte(todoUID(t) + t.Title + t.RRule + t.Group + fmt.Sprintf("%d", t.Priority) + t.Reminders + t.UpdatedAt.String()))
	return hex.EncodeToString(sum[:8])
}

// toObject renders one calendar resource containing the master event and all
// its exceptions (same UID, RFC 4791 §4.1).
func (b *davBackend) toObject(ctx context.Context, master *models.CalendarEvent, exs []models.CalendarEvent, cal string) (*caldav.CalendarObject, error) {
	s := b.session(ctx)
	all := make([]models.CalendarEvent, 0, 1+len(exs))
	all = append(all, *master)
	all = append(all, exs...)
	mod := master.UpdatedAt
	for i := range all {
		if cal == calTeam && all[i].Visibility == models.VisibilityBusy && all[i].UserID != s.User.ID {
			all[i].Title = "忙碌"
			all[i].Location = ""
			all[i].Description = ""
		}
		if all[i].UpdatedAt.After(mod) {
			mod = all[i].UpdatedAt
		}
	}
	ics := BuildCalendar(s.Team.Name, all)
	return &caldav.CalendarObject{
		Path:          calendarPath(s.Email, cal) + master.UID + ".ics",
		ModTime:       mod,
		ContentLength: int64(len(serialize(ics))),
		ETag:          bundleEtag(all),
		Data:          ics,
	}, nil
}

// expandObject expands a recurring master (and its exceptions) into individual
// VEVENT instances within [exp.Start, exp.End) per RFC 4791 §9.6.5. Each
// instance is a separate VEVENT with a concretized DTSTART/DTEND and a
// RECURRENCE-ID (omitted for the initial occurrence).
func (b *davBackend) expandObject(ctx context.Context, master *models.CalendarEvent, exs []models.CalendarEvent, cal string, exp *caldav.CalendarExpandRequest) (*caldav.CalendarObject, error) {
	s := b.session(ctx)
	start, end := exp.Start, exp.End
	dur := master.EndsAt.Sub(master.StartsAt)
	rr, err := parseRuleWithStart(master.RRule, master.StartsAt)
	if err != nil {
		return b.toObject(ctx, master, exs, cal)
	}

	exByRID := map[string]models.CalendarEvent{}
	for i := range exs {
		if exs[i].RecurrenceID != nil {
			exByRID[exs[i].RecurrenceID.UTC().Format(icalUTCFormat)] = exs[i]
		}
	}

	var instances []models.CalendarEvent
	for _, occ := range rr.Between(start.Add(-dur), end, true) {
		key := occ.UTC().Format(icalUTCFormat)
		if ex, ok := exByRID[key]; ok {
			instances = append(instances, ex)
			continue
		}
		inst := *master
		inst.StartsAt = occ
		inst.EndsAt = occ.Add(dur)
		inst.RRule = ""
		rid := occ
		inst.RecurrenceID = &rid
		instances = append(instances, inst)
	}

	mod := master.UpdatedAt
	for i := range instances {
		if cal == calTeam && instances[i].Visibility == models.VisibilityBusy && instances[i].UserID != s.User.ID {
			instances[i].Title = "忙碌"
			instances[i].Location = ""
			instances[i].Description = ""
		}
		if instances[i].UpdatedAt.After(mod) {
			mod = instances[i].UpdatedAt
		}
	}
	ics := BuildCalendar(s.Team.Name, instances)
	return &caldav.CalendarObject{
		Path:          calendarPath(s.Email, cal) + master.UID + ".ics",
		ModTime:       mod,
		ContentLength: int64(len(serialize(ics))),
		ETag:          bundleEtag(instances),
		Data:          ics,
	}, nil
}

// expandTodoObject expands a recurring VTODO into its occurrences within
// [exp.Start, exp.End], rendering each as a VTODO with DTSTART/DUE set to the
// occurrence and a RECURRENCE-ID (RFC 4791 §9.6.5).
func (b *davBackend) expandTodoObject(ctx context.Context, t *models.Todo, cal string, exp *caldav.CalendarExpandRequest) (*caldav.CalendarObject, error) {
	s := b.session(ctx)
	rr, err := parseRuleWithStart(t.RRule, *t.StartAt)
	if err != nil {
		return b.toTodoObject(ctx, t, cal)
	}

	var dur time.Duration
	if t.DueAt != nil {
		dur = t.DueAt.Sub(*t.StartAt)
	}

	calOut := newCalendar(s.Team.Name)
	for _, occ := range rr.Between(exp.Start, exp.End, true) {
		inst := *t
		inst.StartAt = &occ
		if dur > 0 {
			due := occ.Add(dur)
			inst.DueAt = &due
		}
		inst.RRule = ""
		comp := todoComponent(&inst)
		rid := ical.NewProp(ical.PropRecurrenceID)
		rid.SetDateTime(occ)
		comp.Props.Set(rid)
		calOut.Children = append(calOut.Children, comp)
	}
	if len(calOut.Children) == 0 {
		return b.toTodoObject(ctx, t, cal)
	}
	return &caldav.CalendarObject{
		Path:          calendarPath(s.Email, cal) + todoUID(t) + ".ics",
		ModTime:       t.UpdatedAt,
		ContentLength: int64(len(serialize(calOut))),
		ETag:          todoEtag(t),
		Data:          calOut,
	}, nil
}

// bundleEvents groups events by UID and returns one CalendarObject per UID
// (master + exceptions merged into a single .ics resource).
func (b *davBackend) bundleEvents(ctx context.Context, evs []models.CalendarEvent, cal string) (map[string]*caldav.CalendarObject, error) {
	byUID := map[string][]models.CalendarEvent{}
	for i := range evs {
		ev := evs[i]
		byUID[ev.UID] = append(byUID[ev.UID], ev)
	}
	out := make(map[string]*caldav.CalendarObject, len(byUID))
	for uid, group := range byUID {
		var master *models.CalendarEvent
		exs := []models.CalendarEvent{}
		for i := range group {
			if group[i].RecurrenceID == nil && master == nil {
				master = &group[i]
			} else {
				exs = append(exs, group[i])
			}
		}
		if master == nil {
			master = &group[0]
			exs = group[1:]
		}
		obj, err := b.toObject(ctx, master, exs, cal)
		if err != nil {
			return nil, err
		}
		out[uid] = obj
	}
	return out, nil
}

func (b *davBackend) toTodoObject(ctx context.Context, t *models.Todo, cal string) (*caldav.CalendarObject, error) {
	s := b.session(ctx)
	ics := BuildTodoCalendar(s.Team.Name, t)
	return &caldav.CalendarObject{
		Path:          calendarPath(s.Email, cal) + todoUID(t) + ".ics",
		ModTime:       t.UpdatedAt,
		ContentLength: int64(len(serialize(ics))),
		ETag:          todoEtag(t),
		Data:          ics,
	}, nil
}

func (b *davBackend) GetCalendarObject(ctx context.Context, p string, req *caldav.CalendarCompRequest) (*caldav.CalendarObject, error) {
	cal := calNameFromPath(p)
	if cal == "" {
		return nil, errNotFound
	}
	evs, todos, err := b.teamItems(ctx, cal)
	if err != nil {
		return nil, err
	}
	uid := eventUID(p)
	var master *models.CalendarEvent
	exs := []models.CalendarEvent{}
	for i := range evs {
		if evs[i].UID != uid {
			continue
		}
		if evs[i].RecurrenceID == nil && master == nil {
			master = &evs[i]
		} else {
			exs = append(exs, evs[i])
		}
	}
	if master != nil {
		if req != nil && req.Expand != nil {
			return b.expandObject(ctx, master, exs, cal, req.Expand)
		}
		return b.toObject(ctx, master, exs, cal)
	}
	for i := range todos {
		if todoUID(&todos[i]) == uid {
			return b.toTodoObject(ctx, &todos[i], cal)
		}
	}
	return nil, errNotFound
}

func (b *davBackend) ListCalendarObjects(ctx context.Context, p string, req *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	cal := calNameFromPath(p)
	if cal == "" {
		return nil, nil
	}
	evs, todos, err := b.teamItems(ctx, cal)
	if err != nil {
		return nil, err
	}
	objs, err := b.bundleEvents(ctx, evs, cal)
	if err != nil {
		return nil, err
	}
	out := make([]caldav.CalendarObject, 0, len(objs)+len(todos))
	for _, o := range objs {
		out = append(out, *o)
	}
	for i := range todos {
		obj, err := b.toTodoObject(ctx, &todos[i], cal)
		if err != nil {
			return nil, err
		}
		out = append(out, *obj)
	}
	return out, nil
}

func queryRange(f *caldav.CompFilter) (time.Time, time.Time) {
	start, end := f.Start, f.End
	for i := range f.Comps {
		// A time-range on a VALARM comp-filter bounds the alarm trigger, not
		// the parent event; it must not leak into the DB-level event filter.
		if f.Comps[i].Name == ical.CompAlarm {
			continue
		}
		cs, ce := queryRange(&f.Comps[i])
		if cs.IsZero() {
			cs = start
		}
		if ce.IsZero() {
			ce = end
		}
		start, end = cs, ce
	}
	return start, end
}

func (b *davBackend) QueryCalendarObjects(ctx context.Context, p string, query *caldav.CalendarQuery) ([]caldav.CalendarObject, error) {
	cal := calNameFromPath(p)
	if cal == "" {
		return nil, nil
	}
	if _, err := b.GetCalendar(ctx, p); err != nil {
		return nil, err
	}
	evs, todos, err := b.teamItems(ctx, cal)
	if err != nil {
		return nil, err
	}
	start, end := queryRange(&query.CompFilter)
	// Group events by UID so master + exceptions are returned as one resource.
	byUID := map[string][]models.CalendarEvent{}
	for i := range evs {
		byUID[evs[i].UID] = append(byUID[evs[i].UID], evs[i])
	}
	out := []caldav.CalendarObject{}
	for _, group := range byUID {
		var master *models.CalendarEvent
		exs := []models.CalendarEvent{}
		for i := range group {
			if group[i].RecurrenceID == nil && master == nil {
				master = &group[i]
			} else {
				exs = append(exs, group[i])
			}
		}
		if master == nil {
			master = &group[0]
			exs = group[1:]
		}
		if !groupInRange(master, exs, start, end) {
			continue
		}
		doExpand := query != nil && query.CompRequest.Expand != nil
		if doExpand && master.RRule != "" {
			obj, err := b.expandObject(ctx, master, exs, cal, query.CompRequest.Expand)
			if err != nil {
				return nil, err
			}
			out = append(out, *obj)
		} else {
			obj, err := b.toObject(ctx, master, exs, cal)
			if err != nil {
				return nil, err
			}
			out = append(out, *obj)
		}
	}
	for i := range todos {
		t := &todos[i]
		if !todoInRange(t, start, end) {
			continue
		}
		doExpand := query != nil && query.CompRequest.Expand != nil
		if doExpand && t.RRule != "" && t.StartAt != nil {
			obj, err := b.expandTodoObject(ctx, t, cal, query.CompRequest.Expand)
			if err != nil {
				return nil, err
			}
			out = append(out, *obj)
		} else {
			obj, err := b.toTodoObject(ctx, t, cal)
			if err != nil {
				return nil, err
			}
			out = append(out, *obj)
		}
	}
	if query == nil {
		return out, nil
	}
	// caldav.Filter also applies time-range matching, but its
	// matchCompTimeRange only handles VEVENT and would drop every VTODO.
	// We already filtered by time-range above, so strip the range and let the
	// filter match comp-type / text-match only (i;octet case-sensitive).
	strip := &caldav.CalendarQuery{CompRequest: query.CompRequest}
	strip.CompFilter = withoutTimeRange(query.CompFilter)
	return filterCalendarObjects(strip, out)
}

const icalUTCFormat = "20060102T150405Z"

// QueryFreeBusy returns the busy periods of a calendar collection within
// [q.Start, q.End] as a VFREEBUSY calendar (RFC 4791 §7.10).
func (b *davBackend) QueryFreeBusy(ctx context.Context, p string, q *caldav.FreeBusyQuery) (*ical.Calendar, error) {
	cal := calNameFromPath(p)
	if cal == "" {
		cal = calSelf
	}
	evs, _, err := b.teamItems(ctx, cal)
	if err != nil {
		return nil, err
	}

	start, end := q.Start, q.End
	s := b.session(ctx)
	cal_out := ical.NewCalendar()
	cal_out.Props.SetText(ical.PropVersion, "2.0")
	cal_out.Props.SetText(ical.PropProductID, "-//HomiHub//Team Calendar//CN")

	fb := ical.NewComponent(ical.CompFreeBusy)
	fb.Props.SetText(ical.PropUID, "freebusy-"+s.User.ID+"-"+start.UTC().Format(icalUTCFormat))
	fb.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	fb.Props.SetDateTime(ical.PropDateTimeStart, start)
	fb.Props.SetDateTime(ical.PropDateTimeEnd, end)
	fb.Props.SetText(ical.PropOrganizer, "mailto:"+s.Email)

	periods := b.freeBusyPeriods(evs, start, end)
	for _, p := range periods {
		prop := ical.NewProp(ical.PropFreeBusy)
		prop.Value = p[0].UTC().Format(icalUTCFormat) + "/" + p[1].UTC().Format(icalUTCFormat)
		fb.Props.Add(prop)
	}
	cal_out.Children = append(cal_out.Children, fb)
	return cal_out, nil
}

// freeBusyPeriods computes the [start,end) busy intervals for events
// overlapping the query range, expanding recurring masters and applying
// exceptions by RECURRENCE-ID.
func (b *davBackend) freeBusyPeriods(evs []models.CalendarEvent, start, end time.Time) [][2]time.Time {
	// First pass: exceptions indexed by (UID, RECURRENCE-ID).
	exceptions := map[string]models.CalendarEvent{}
	masters := map[string]*models.CalendarEvent{}
	var singles []models.CalendarEvent
	for i := range evs {
		ev := &evs[i]
		if ev.RecurrenceID != nil {
			exceptions[ev.UID+"|"+ev.RecurrenceID.UTC().Format(icalUTCFormat)] = *ev
			continue
		}
		if ev.RRule != "" {
			masters[ev.UID] = ev
		} else {
			singles = append(singles, *ev)
		}
	}

	var periods [][2]time.Time
	add := func(s, e time.Time) {
		if e.Before(s) {
			e = s
		}
		if !e.After(start) || !s.Before(end) {
			return
		}
		if s.Before(start) {
			s = start
		}
		if e.After(end) {
			e = end
		}
		periods = append(periods, [2]time.Time{s, e})
	}

	for _, ev := range singles {
		add(ev.StartsAt, ev.EndsAt)
	}

	for uid, master := range masters {
		dur := master.EndsAt.Sub(master.StartsAt)
		rr, err := parseRuleWithStart(master.RRule, master.StartsAt)
		if err != nil {
			add(master.StartsAt, master.EndsAt)
			continue
		}
		for _, occ := range rr.Between(start.Add(-dur), end, true) {
			key := uid + "|" + occ.UTC().Format(icalUTCFormat)
			if ex, ok := exceptions[key]; ok {
				add(ex.StartsAt, ex.EndsAt)
			} else {
				add(occ, occ.Add(dur))
			}
		}
	}
	return periods
}

// SearchPrincipals searches team members by display name per RFC 3744 §8.6.
func (b *davBackend) SearchPrincipals(ctx context.Context, query *caldav.PrincipalSearchQuery) ([]caldav.PrincipalSearchResult, error) {
	s := b.session(ctx)
	var users []models.User
	if err := middleware.ScopedDB(b.app.DB, s.Team.ID).Find(&users).Error; err != nil {
		return nil, err
	}
	var out []caldav.PrincipalSearchResult
	for i := range users {
		u := &users[i]
		if len(query.Terms) > 0 {
			match := false
			for _, t := range query.Terms {
				if strings.Contains(u.Name, t) || strings.Contains(u.Email, t) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, caldav.PrincipalSearchResult{
			Path:            principalPath(u.Email),
			DisplayName:     u.Name,
			CalendarHomeSet: "/dav/" + url.PathEscape(u.Email) + "/calendars/",
		})
	}
	return out, nil
}

// groupInRange reports whether the master event or any exception overlaps the
// time range (the whole UID resource is returned when any instance matches).
func groupInRange(master *models.CalendarEvent, exs []models.CalendarEvent, start, end time.Time) bool {
	if inRange(master, start, end) {
		return true
	}
	for i := range exs {
		if inRange(&exs[i], start, end) {
			return true
		}
	}
	return false
}

// withoutTimeRange returns a deep copy of f with all time-range bounds cleared,
// except on VALARM comp-filters (the DB layer has no alarm trigger to index, so
// the filter layer must evaluate those; see matchCIFilter).
func withoutTimeRange(f caldav.CompFilter) caldav.CompFilter {
	g := f
	g.Start, g.End = time.Time{}, time.Time{}
	g.Props = make([]caldav.PropFilter, len(f.Props))
	for i, p := range f.Props {
		g.Props[i] = p
		g.Props[i].Start, g.Props[i].End = time.Time{}, time.Time{}
	}
	g.Comps = make([]caldav.CompFilter, len(f.Comps))
	for i := range f.Comps {
		if f.Comps[i].Name == ical.CompAlarm {
			g.Comps[i] = f.Comps[i]
		} else {
			g.Comps[i] = withoutTimeRange(f.Comps[i])
		}
	}
	return g
}

// todoInRange reports whether a VTODO overlaps a time range per RFC4791 §9.9.
// The model tracks DTSTART (StartAt) and DUE (DueAt); a VTODO with a DURATION
// but no DUE is folded by ParseTodo so that only DTSTART applies.  Recurring
// VTODOs (RRULE) match when any implicit instance falls within the range.
func todoInRange(t *models.Todo, start, end time.Time) bool {
	if start.IsZero() && end.IsZero() {
		return true
	}
	if t.RRule != "" && t.StartAt != nil {
		rr, err := parseRuleWithStart(t.RRule, *t.StartAt)
		if err == nil && len(rr.Between(start, end, true)) > 0 {
			return true
		}
	}
	switch {
	case t.StartAt != nil && t.DueAt != nil:
		// RFC4791 §9.9 row: DTSTART=Y, DURATION=N, DUE=Y
		// ((start < DUE) OR (start <= DTSTART)) AND ((end > DTSTART) OR (end >= DUE))
		return (rfcStartLt(start, *t.DueAt) || rfcStartLe(start, *t.StartAt)) &&
			(rfcEndGt(end, *t.StartAt) || rfcEndGe(end, *t.DueAt))
	case t.StartAt != nil:
		// RFC4791 §9.9 row: DTSTART=Y, DURATION=N, DUE=N
		// (start <= DTSTART) AND (end > DTSTART)
		return rfcStartLe(start, *t.StartAt) && rfcEndGt(end, *t.StartAt)
	case t.DueAt != nil:
		// RFC4791 §9.9 row: DTSTART=N, DURATION=N, DUE=Y
		// (start < DUE) AND (end >= DUE)
		return rfcStartLt(start, *t.DueAt) && rfcEndGe(end, *t.DueAt)
	case t.Completed:
		// RFC4791 §9.9 row: DTSTART=N, DURATION=N, DUE=N, COMPLETED=Y
		// ((start <= CREATED) OR (start <= COMPLETED)) AND
		// ((end >= CREATED) OR (end >= COMPLETED))
		c := t.CreatedAt
		done := t.CompletedAt
		if done == nil {
			return false
		}
		return (rfcStartLe(start, c) || rfcStartLe(start, *done)) &&
			(rfcEndGe(end, c) || rfcEndGe(end, *done))
	default:
		// RFC4791 §9.9 row: no DTSTART/DURATION/DUE/COMPLETED.
		// (end > CREATED)
		return rfcEndGt(end, t.CreatedAt)
	}
}

// The RFC uses a lower bound that is inclusive of `start` and an upper bound
// exclusive of `end`. A zero time.Time means that side of the range is unset
// (treated as unbounded).
func rfcStartLe(start, x time.Time) bool {
	return start.IsZero() || !start.After(x)
}

func rfcStartLt(start, x time.Time) bool {
	return start.IsZero() || start.Before(x)
}

func rfcEndGt(end, x time.Time) bool {
	return end.IsZero() || end.After(x)
}

func rfcEndGe(end, x time.Time) bool {
	return end.IsZero() || !end.Before(x)
}

func inRange(ev *models.CalendarEvent, start, end time.Time) bool {
	if start.IsZero() && end.IsZero() {
		return true
	}
	if ev.ComponentType == ical.CompJournal {
		// VJOURNAL has a DTSTART but no DTEND; it matches when its DTSTART
		// falls within the range (RFC 4791 §9.9 row for VJOURNAL).
		return !ev.StartsAt.Before(start) && !ev.StartsAt.After(end)
	}
	if ev.RecurrenceID != nil {
		return !ev.StartsAt.Before(start) && !ev.StartsAt.After(end)
	}
	if ev.RRule == "" {
		return !ev.EndsAt.Before(start) && !ev.StartsAt.After(end)
	}
	rr, err := parseRuleWithStart(ev.RRule, ev.StartsAt)
	if err != nil {
		return !ev.EndsAt.Before(start) && !ev.StartsAt.After(end)
	}
	return len(rr.Between(start, end, true)) > 0
}

// PutCalendarObject: mobile writes an .ics back into the DB (two-way sync).
// Events PUT to the self calendar are forced VisibilityPrivate; VTODO payloads
// are stored as personal todos.  Multi-VEVENT payloads (master + RECURRENCE-ID
// exceptions per RFC 4791 §4.1) are all stored; any DB exception rows not in
// the payload are removed when the payload includes the master.
func (b *davBackend) PutCalendarObject(ctx context.Context, p string, cal *ical.Calendar, opts *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	s := b.session(ctx)
	calName := calNameFromPath(p)
	switch comp := componentOf(cal); comp {
	case ical.CompToDo:
		if !b.supportsComponent(ctx, calName, ical.CompToDo) {
			return nil, caldav.NewPreconditionError(caldav.PreconditionSupportedCalendarComponent)
		}
		pt, err := ParseTodo(cal)
		if err != nil {
			return nil, err
		}
		co, err := b.putTodo(ctx, p, calName, pt)
		if err != nil {
			return nil, err
		}
		_, _ = syncLogChange(b.app.DB, s.Team.ID, calName, p, co.ETag, false)
		return co, nil
	case ical.CompJournal:
		if !b.supportsComponent(ctx, calName, ical.CompJournal) {
			return nil, caldav.NewPreconditionError(caldav.PreconditionSupportedCalendarComponent)
		}
		if journals := ParseJournals(cal); len(journals) > 0 {
			return b.putJournal(ctx, p, calName, journals)
		}
		return nil, errors.New("无效的 VJOURNAL 数据")
	case ical.CompFreeBusy:
		return nil, caldav.NewPreconditionError(caldav.PreconditionSupportedCalendarComponent)
	}
	if !b.supportsComponent(ctx, calName, ical.CompEvent) {
		return nil, caldav.NewPreconditionError(caldav.PreconditionSupportedCalendarComponent)
	}
	parsed := ParseEvents(cal)
	if len(parsed) == 0 {
		// Check if calendar contains only unsupported components (VFREEBUSY).
		// Return 409 with PreconditionSupportedCalendarComponent instead of 500.
		for _, c := range cal.Children {
			if c.Name == ical.CompFreeBusy {
				return nil, caldav.NewPreconditionError(caldav.PreconditionSupportedCalendarComponent)
			}
		}
		return nil, errors.New("无效的 iCalendar 数据")
	}
	vis := models.VisibilityTeam
	if calName == calSelf {
		vis = models.VisibilityPrivate
	}
	// Each chain gets a fresh scoped session: GORM shares the underlying
	// statement (clone==0) and reusing one handle across chains merges WHERE
	// conditions, which corrupts subsequent writes (e.g. Create inheriting a
	// "recurrence_id IS NULL" condition returns ErrRecordNotFound).
	sc := func() *gorm.DB { return middlewareScopedDB(b.app.DB, s.Team.ID) }
	uid := parsed[0].UID
	hasMaster := false
	incomingRIDs := map[time.Time]bool{}
	for i := range parsed {
		pe := &parsed[i]
		if pe.RecurrenceID == nil {
			hasMaster = true
		} else {
			incomingRIDs[*pe.RecurrenceID] = true
		}
		var ev models.CalendarEvent
		q := sc().Where("uid = ? AND calendar = ?", pe.UID, calName)
		if pe.RecurrenceID != nil {
			q = q.Where("recurrence_id IS NOT NULL AND recurrence_id = ?", pe.RecurrenceID.UTC())
		} else {
			q = q.Where("recurrence_id IS NULL")
		}
		if err := q.First(&ev).Error; err == nil {
			ev.Title = pe.Title
			ev.Location = pe.Location
			ev.Description = pe.Description
			ev.Category = pe.Category
			ev.Class = pe.Class
			ev.Duration = pe.Duration
			ev.StartsAt = pe.StartsAt
			ev.EndsAt = pe.EndsAt
			ev.AllDay = pe.AllDay
			ev.RRule = pe.RRule
			ev.ExDate = exDatesJoin(pe.ExDates)
			ev.RecurrenceID = pe.RecurrenceID
			ev.RelatedTo = pe.RelatedTo
			ev.Visibility = vis
			ev.Calendar = calName
			ev.Reminders = remindersJSON(pe.Reminders)
			ev.UpdatedAt = time.Now()
			if err := sc().Save(&ev).Error; err != nil {
				return nil, fmt.Errorf("caldav save: %w", err)
			}
		} else {
			ev = models.CalendarEvent{
				ID:           uuid.Must(uuid.NewV7()).String(),
				TeamID:       s.Team.ID,
				UserID:       s.User.ID,
				UID:          pe.UID,
				Calendar:     calName,
				Title:        pe.Title,
				Location:     pe.Location,
				Description:  pe.Description,
				Category:     pe.Category,
				Class:        pe.Class,
				Duration:     pe.Duration,
				StartsAt:     pe.StartsAt,
				EndsAt:       pe.EndsAt,
				AllDay:       pe.AllDay,
				RRule:        pe.RRule,
				ExDate:       exDatesJoin(pe.ExDates),
				RecurrenceID: pe.RecurrenceID,
				RelatedTo:    pe.RelatedTo,
				Visibility:   vis,
				Reminders:    remindersJSON(pe.Reminders),
			}
			if err := sc().Create(&ev).Error; err != nil {
				return nil, fmt.Errorf("caldav create: %w", err)
			}
		}
	}
	// If the payload includes a master, reconcile DB exceptions: remove any
	// exception rows not in the incoming set.
	if hasMaster {
		var existing []models.CalendarEvent
		sc().Where("uid = ? AND calendar = ? AND recurrence_id IS NOT NULL", uid, calName).Find(&existing)
		for _, ex := range existing {
			if !incomingRIDs[*ex.RecurrenceID] {
				sc().Delete(&ex)
			}
		}
	}
	// Auto-schedule: deliver the event to any registered attendee's inbox and
	// copy it into their personal calendar (RFC 6638 SCHEDULE-AGENT=SERVER).
	b.deliverToInboxAndAutoSchedule(ctx, cal)
	// Reload the full bundle and return a single object.
	var master models.CalendarEvent
	if err := sc().Where("uid = ? AND calendar = ? AND recurrence_id IS NULL", uid, calName).First(&master).Error; err != nil {
		// No master row (edge case: payload was all exceptions). Rebuild the
		// bundle from whatever rows exist for this UID.
		var uids []models.CalendarEvent
		sc().Where("uid = ? AND calendar = ?", uid, calName).Find(&uids)
		if len(uids) == 0 {
			return nil, errNotFound
		}
		objs, err := b.bundleEvents(ctx, uids, calName)
		if err != nil {
			return nil, err
		}
		if o, ok := objs[uid]; ok {
			o.Path = p
			_, _ = syncLogChange(b.app.DB, s.Team.ID, calName, p, o.ETag, false)
			return o, nil
		}
		return nil, errNotFound
	}
	var exs []models.CalendarEvent
	sc().Where("uid = ? AND calendar = ? AND recurrence_id IS NOT NULL", uid, calName).Find(&exs)
	obj, err := b.toObject(ctx, &master, exs, calName)
	if err != nil {
		return nil, err
	}
	// The response path must match the PUT request path.
	obj.Path = p
	_, _ = syncLogChange(b.app.DB, s.Team.ID, calName, p, obj.ETag, false)
	return obj, nil
}

// putTodo stores a VTODO written by an external CalDAV client (tasks.org,
// Apple Reminders, ...). Personal todos go into the self calendar; team and
// custom calendar todos are visible to all team members.
func (b *davBackend) putTodo(ctx context.Context, p, calName string, pt *parsedTodo) (*caldav.CalendarObject, error) {
	s := b.session(ctx)
	if pt.UID == "" || pt.Title == "" {
		return nil, errors.New("无效的 VTODO 数据")
	}
	// Each chain gets a fresh scoped session: GORM shares the underlying
	// statement (clone==0) and reusing one handle across chains merges WHERE
	// conditions, which corrupts subsequent writes (e.g. Create inheriting a
	// prior WHERE returns ErrRecordNotFound).
	sc := func() *gorm.DB { return middlewareScopedDB(b.app.DB, s.Team.ID) }
	var t models.Todo
	if err := sc().Where("uid = ? AND calendar = ?", pt.UID, calName).First(&t).Error; err == nil {
		if calName == calSelf && t.UserID != s.User.ID {
			return nil, errors.New("不能修改他人的待办")
		}
		t.Title = pt.Title
		t.Note = pt.Note
		t.Location = pt.Location
		t.URL = pt.URL
		t.StartAt = pt.StartsAt
		t.DueAt = pt.DueAt
		t.Completed = pt.Completed
		t.Percent = pt.Percent
		t.Priority = pt.Priority
		t.RRule = pt.RRule
		t.ExDate = exDatesJoin(pt.ExDates)
		t.Group = pt.Group
		t.Tags = tagsToCSV(pt.Tags)
		t.ParentID = pt.ParentUID
		t.CompletedAt = pt.CompletedAt
		if len(pt.Reminders) > 0 {
			if b, err := json.Marshal(pt.Reminders); err == nil {
				t.Reminders = string(b)
			}
		} else {
			t.Reminders = ""
		}
		t.Calendar = calName
		t.UpdatedAt = time.Now()
		if err := sc().Save(&t).Error; err != nil {
			return nil, fmt.Errorf("caldav save todo: %w", err)
		}
		recordTodoLog(sc(), s.Team.ID, s.User.ID, t.ID, "update", "")
	} else {
		uid := pt.UID
		if uid == "" {
			uid = "todo-" + uuid.Must(uuid.NewV7()).String()
		}
		t = models.Todo{
			ID:          uuid.Must(uuid.NewV7()).String(),
			TeamID:      s.Team.ID,
			UserID:      s.User.ID,
			UID:         uid,
			Calendar:    calName,
			Title:       pt.Title,
			Note:        pt.Note,
			Location:    pt.Location,
			URL:         pt.URL,
			StartAt:     pt.StartsAt,
			DueAt:       pt.DueAt,
			Completed:   pt.Completed,
			Percent:     pt.Percent,
			Priority:    pt.Priority,
			RRule:       pt.RRule,
			ExDate:      exDatesJoin(pt.ExDates),
			Group:       pt.Group,
			Tags:        tagsToCSV(pt.Tags),
			ParentID:    pt.ParentUID,
			CompletedAt: pt.CompletedAt,
		}
		if len(pt.Reminders) > 0 {
			if b, err := json.Marshal(pt.Reminders); err == nil {
				t.Reminders = string(b)
			}
		}
		if err := sc().Create(&t).Error; err != nil {
			return nil, fmt.Errorf("caldav create todo: %w", err)
		}
		recordTodoLog(sc(), s.Team.ID, s.User.ID, t.ID, "create", "")
	}
	return b.toTodoObject(ctx, &t, calName)
}

// recordTodoLog appends a mutation to the team's todo audit trail.
func recordTodoLog(db *gorm.DB, teamID, userID, todoID, action, details string) {
	tl := models.TodoLog{
		TeamID:  teamID,
		TodoID:  todoID,
		UserID:  userID,
		Action:  action,
		Details: details,
	}
	_ = db.Create(&tl).Error
}

// putJournal stores a VJOURNAL written by an external CalDAV client as a
// CalendarEvent row with ComponentType=VJOURNAL so it round-trips and shows up
// in REPORT searches (RFC 4791 §5.1).
func (b *davBackend) putJournal(ctx context.Context, p, calName string, journals []parsedEvent) (*caldav.CalendarObject, error) {
	s := b.session(ctx)
	vis := models.VisibilityTeam
	if calName == calSelf {
		vis = models.VisibilityPrivate
	}
	sc := func() *gorm.DB { return middlewareScopedDB(b.app.DB, s.Team.ID) }
	var master *models.CalendarEvent
	for i := range journals {
		pe := &journals[i]
		var ev models.CalendarEvent
		if err := sc().Where("uid = ? AND calendar = ? AND component_type = ?", pe.UID, calName, ical.CompJournal).First(&ev).Error; err == nil {
			ev.Title = pe.Title
			ev.Description = pe.Description
			ev.StartsAt = pe.StartsAt
			ev.UpdatedAt = time.Now()
			if err := sc().Save(&ev).Error; err != nil {
				return nil, fmt.Errorf("caldav save journal: %w", err)
			}
			master = &ev
		} else {
			ev = models.CalendarEvent{
				ID:            uuid.Must(uuid.NewV7()).String(),
				TeamID:        s.Team.ID,
				UserID:        s.User.ID,
				UID:           pe.UID,
				Calendar:      calName,
				ComponentType: ical.CompJournal,
				Title:         pe.Title,
				Description:   pe.Description,
				StartsAt:      pe.StartsAt,
				Visibility:    vis,
			}
			if err := sc().Create(&ev).Error; err != nil {
				return nil, fmt.Errorf("caldav create journal: %w", err)
			}
			master = &ev
		}
	}
	if master == nil {
		return nil, errors.New("无效的 VJOURNAL 数据")
	}
	obj, err := b.toObject(ctx, master, nil, calName)
	if err != nil {
		return nil, err
	}
	obj.Path = p
	_, _ = syncLogChange(b.app.DB, s.Team.ID, calName, p, obj.ETag, false)
	return obj, nil
}

func (b *davBackend) DeleteCalendarObject(ctx context.Context, p string) error {
	// A DELETE on the calendar collection itself removes the whole calendar.
	if strings.HasSuffix(p, "/") {
		return b.DeleteCalendar(ctx, p)
	}
	cal := calNameFromPath(p)
	if cal == "" {
		return nil
	}
	_, todos, err := b.teamItems(ctx, cal)
	if err != nil {
		return err
	}
	uid := eventUID(p)
	s := b.session(ctx)
	sc := func() *gorm.DB { return middlewareScopedDB(b.app.DB, s.Team.ID) }
	for i := range todos {
		if todoUID(&todos[i]) == uid {
			t := todos[i]
			// Scope the delete by calendar so the same UID in another
			// calendar (e.g. a custom task list) is untouched.
			err := sc().Where("uid = ? AND calendar = ?", uid, t.Calendar).Delete(&models.Todo{}).Error
			if err == nil {
				recordTodoLog(sc(), s.Team.ID, s.User.ID, t.ID, "delete", "")
				_, _ = syncLogChange(b.app.DB, s.Team.ID, cal, p, "", true)
			}
			return err
		}
	}
	// Check events exist before deleting so we don't log phantom deletes.
	var count int64
	sc().Model(&models.CalendarEvent{}).Where("uid = ? AND calendar = ?", uid, cal).Count(&count)
	if count == 0 {
		return nil
	}
	err = sc().Where("uid = ? AND calendar = ?", uid, cal).Delete(&models.CalendarEvent{}).Error
	if err == nil {
		_, _ = syncLogChange(b.app.DB, s.Team.ID, cal, p, "", true)
	}
	return err
}

// davAuth: Basic Auth = member email + (family calendar_token OR member password).
func (h *Handler) davAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		email, pass, ok := c.Request.BasicAuth()
		if !ok {
			c.Header("WWW-Authenticate", `Basic realm="HomiHub CalDAV"`)
			httpx.UnauthorizedT(c, "calendar_auth_required")
			c.Abort()
			return
		}
		pass, err := url.QueryUnescape(pass)
		if err != nil {
			httpx.UnauthorizedT(c, "calendar_auth_required")
			c.Abort()
			return
		}
		var fam models.Team
		tokenOK := h.app.DB.Where("calendar_token = ?", pass).First(&fam).Error == nil
		if !tokenOK {
			var u models.User
			if err := h.app.DB.Where("email = ?", email).First(&u).Error; err != nil ||
				bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(pass)) != nil {
				c.Header("WWW-Authenticate", `Basic realm="HomiHub CalDAV"`)
				httpx.UnauthorizedT(c, "calendar_auth_required")
				c.Abort()
				return
			}
			if err := h.app.DB.Where("id = ?", u.TeamID).First(&fam).Error; err != nil {
				c.Header("WWW-Authenticate", `Basic realm="HomiHub CalDAV"`)
				httpx.UnauthorizedT(c, "calendar_auth_required")
				c.Abort()
				return
			}
		}
		var user models.User
		if err := h.app.DB.Where("email = ?", email).First(&user).Error; err != nil {
			c.Header("WWW-Authenticate", `Basic realm="HomiHub CalDAV"`)
			httpx.UnauthorizedT(c, "calendar_auth_required")
			c.Abort()
			return
		}
		var uf models.TeamMember
		if err := h.app.DB.Where("user_id = ? AND team_id = ?", user.ID, fam.ID).First(&uf).Error; err != nil {
			c.Header("WWW-Authenticate", `Basic realm="HomiHub CalDAV"`)
			httpx.UnauthorizedT(c, "calendar_auth_required")
			c.Abort()
			return
		}
		user.Role = uf.Role
		ctx := context.WithValue(c.Request.Context(), davKey{}, &DavSession{Team: &fam, Email: email, User: &user})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// DavSession is the exported session view for other DAV backends (files WebDAV).
func SessionFromContext(ctx context.Context) *DavSession {
	s, _ := ctx.Value(davKey{}).(*DavSession)
	return s
}

// RegisterDAV mounts the CalDAV handler (and the files WebDAV handler when
// provided) on the /dav prefix.
func (h *Handler) RegisterDAV(r *gin.RouterGroup, files http.Handler) {
	calBackend := &davBackend{app: h.app}
	dh := &caldav.Handler{Backend: calBackend, Prefix: "/dav"}
	auth := h.davAuth()
	mux := &davMux{caldav: &syncDAVHandler{inner: dh, b: calBackend}, files: files}
	davMethods := []string{"GET", "HEAD", "PUT", "DELETE", "OPTIONS", "POST", "PROPFIND", "PROPPATCH", "REPORT", "COPY", "MOVE", "MKCOL", "MKCALENDAR", "LOCK", "UNLOCK"}
	r.Match(davMethods, "/.well-known/caldav", auth, gin.WrapH(davRequestLog(dh)))
	r.Match(davMethods, "/dav", auth, gin.WrapH(davRequestLog(mux)))
	r.Match(davMethods, "/dav/*dav", auth, gin.WrapH(davRequestLog(mux)))
}

type davMux struct {
	caldav http.Handler
	files  http.Handler
}

func (m *davMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/dav/files") {
		if m.files != nil {
			m.files.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	m.caldav.ServeHTTP(w, r)
}

// davRequestLog wraps a handler and logs every DAV request (method, path,
// body, response status) to stdout for debugging client interoperability.
func davRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if rec := recover(); rec != nil {
				fmt.Printf("[dav] %s %s PANIC=%v body=%s\n%s\n", r.Method, r.URL.Path, rec, string(body), debug.Stack())
				panic(rec)
			}
			fmt.Printf("[dav] %s %s status=%d body=%s\n", r.Method, r.URL.Path, sw.status, string(body))
		}()
		next.ServeHTTP(sw, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}
