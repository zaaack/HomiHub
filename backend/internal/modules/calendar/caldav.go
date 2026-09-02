package modulecalendar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	"homihub/backend/internal/modules"
)

const (
	calSelf   = "self"
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
	Team *models.Team
	Email  string
	User   *models.User
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
	return errors.New("不支持创建日历")
}

func (b *davBackend) ListCalendars(ctx context.Context) ([]caldav.Calendar, error) {
	s := b.session(ctx)
	return []caldav.Calendar{
		{
			Path:                  calendarPath(s.Email, calSelf),
			Name:                  "我的",
			Description:           "我的私人日历",
			SupportedComponentSet: []string{ical.CompEvent, ical.CompToDo},
		},
		{
			Path:                  calendarPath(s.Email, calTeam),
			Name:                  s.Team.Name,
			Description:           s.Team.Name + " 的共享日历",
			SupportedComponentSet: []string{ical.CompEvent},
		},
	}, nil
}

func (b *davBackend) GetCalendar(ctx context.Context, p string) (*caldav.Calendar, error) {
	s := b.session(ctx)
	cal := calNameFromPath(p)
	if cal == calSelf {
		return &caldav.Calendar{Path: p, Name: "我的", Description: "我的私人日历", SupportedComponentSet: []string{ical.CompEvent, ical.CompToDo}}, nil
	}
	if cal == calTeam {
		return &caldav.Calendar{Path: p, Name: s.Team.Name, Description: s.Team.Name + " 的共享日历", SupportedComponentSet: []string{ical.CompEvent}}, nil
	}
	return nil, errNotFound
}

// teamItems returns the resources visible in a calendar: VEVENT for both
// calendars, plus the member's personal VTODO in the self calendar.
func (b *davBackend) teamItems(ctx context.Context, cal string) (evs []models.CalendarEvent, todos []models.Todo, err error) {
	s := b.session(ctx)
	switch {
	case cal == calSelf:
		if err = middleware.ScopedDB(b.app.DB, s.Team.ID).
			Where("user_id = ? AND visibility = ?", s.User.ID, models.VisibilityPrivate).
			Order("starts_at").Find(&evs).Error; err != nil {
			return nil, nil, err
		}
		if err = middleware.ScopedDB(b.app.DB, s.Team.ID).
			Where("user_id = ?", s.User.ID).
			Order("created_at").Find(&todos).Error; err != nil {
			return nil, nil, err
		}
	case cal == calTeam:
		if err = middleware.ScopedDB(b.app.DB, s.Team.ID).
			Where("visibility IN ?", []int{models.VisibilityTeam, models.VisibilityBusy}).
			Order("starts_at").Find(&evs).Error; err != nil {
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

func todoEtag(t *models.Todo) string {
	sum := sha256.Sum256([]byte(todoUID(t) + t.Title + t.RRule + t.Group + fmt.Sprintf("%d", t.Priority) + t.Reminders + t.UpdatedAt.String()))
	return hex.EncodeToString(sum[:8])
}

func (b *davBackend) toObject(ctx context.Context, ev *models.CalendarEvent, cal string) (*caldav.CalendarObject, error) {
	s := b.session(ctx)
	masked := *ev
	if cal == calTeam && ev.Visibility == models.VisibilityBusy && ev.UserID != s.User.ID {
		masked.Title = "忙碌"
		masked.Location = ""
		masked.Description = ""
	}
	ics := BuildCalendar(s.Team.Name, []models.CalendarEvent{masked})
	return &caldav.CalendarObject{
		Path:          calendarPath(s.Email, cal) + ev.UID + ".ics",
		ModTime:       ev.UpdatedAt,
		ContentLength: int64(len(serialize(ics))),
		ETag:          eventEtag(ev),
		Data:          ics,
	}, nil
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
	for i := range evs {
		if evs[i].UID == uid {
			return b.toObject(ctx, &evs[i], cal)
		}
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
	out := make([]caldav.CalendarObject, 0, len(evs)+len(todos))
	for i := range evs {
		obj, err := b.toObject(ctx, &evs[i], cal)
		if err != nil {
			return nil, err
		}
		out = append(out, *obj)
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
	evs, todos, err := b.teamItems(ctx, cal)
	if err != nil {
		return nil, err
	}
	start, end := queryRange(&query.CompFilter)
	if end.IsZero() {
		end = time.Now().AddDate(1, 0, 0)
	}
	out := []caldav.CalendarObject{}
	for i := range evs {
		ev := &evs[i]
		if !inRange(ev, start, end) {
			continue
		}
		obj, err := b.toObject(ctx, ev, cal)
		if err != nil {
			return nil, err
		}
		out = append(out, *obj)
	}
	for i := range todos {
		t := &todos[i]
		if !todoInRange(t, start, end) {
			continue
		}
		obj, err := b.toTodoObject(ctx, t, cal)
		if err != nil {
			return nil, err
		}
		out = append(out, *obj)
	}
	if query == nil {
		return out, nil
	}
	// caldav.Filter also applies time-range matching, but its
	// matchCompTimeRange only handles VEVENT and would drop every VTODO.
	// We already filtered by time-range above, so strip the range and let the
	// library match comp-type / text-match only.
	strip := &caldav.CalendarQuery{CompRequest: query.CompRequest}
	strip.CompFilter = withoutTimeRange(query.CompFilter)
	return caldav.Filter(strip, out)
}

// withoutTimeRange returns a deep copy of f with all time-range bounds cleared.
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
		g.Comps[i] = withoutTimeRange(f.Comps[i])
	}
	return g
}

// todoInRange reports whether a VTODO overlaps a time range per RFC4791 §9.9.
// The model tracks DTSTART (StartAt) and DUE (DueAt); a VTODO with a DURATION
// but no DUE is folded by ParseTodo so that only DTSTART applies.
func todoInRange(t *models.Todo, start, end time.Time) bool {
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
// are stored as personal todos.
func (b *davBackend) PutCalendarObject(ctx context.Context, p string, cal *ical.Calendar, opts *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	s := b.session(ctx)
	calName := calNameFromPath(p)
	if pt, err := ParseTodo(cal); err == nil {
		return b.putTodo(ctx, p, calName, pt)
	}
	parsed := ParseEvents(cal)
	if len(parsed) == 0 {
		// Check if calendar contains only unsupported components (VJOURNAL).
		// Return 409 with PreconditionSupportedCalendarComponent instead of 500.
		for _, c := range cal.Children {
			if c.Name == ical.CompJournal || c.Name == ical.CompFreeBusy {
				return nil, caldav.NewPreconditionError(caldav.PreconditionSupportedCalendarComponent)
			}
		}
		return nil, errors.New("无效的 iCalendar 数据")
	}
	pe := parsed[0]
	vis := models.VisibilityTeam
	if calName == calSelf {
		vis = models.VisibilityPrivate
	}
	var ev models.CalendarEvent
	q := middleware.ScopedDB(b.app.DB, s.Team.ID).Where("uid = ?", pe.UID)
	if pe.RecurrenceID != nil {
		q = q.Where("recurrence_id IS NOT NULL AND recurrence_id = ?", pe.RecurrenceID)
	} else {
		q = q.Where("recurrence_id IS NULL")
	}
	if err := q.First(&ev).Error; err == nil {
		ev.Title = pe.Title
		ev.Location = pe.Location
		ev.Description = pe.Description
		ev.Category = pe.Category
		ev.StartsAt = pe.StartsAt
		ev.EndsAt = pe.EndsAt
		ev.AllDay = pe.AllDay
		ev.RRule = pe.RRule
		ev.ExDate = exDatesJoin(pe.ExDates)
		ev.RecurrenceID = pe.RecurrenceID
		ev.RelatedTo = pe.RelatedTo
		ev.Visibility = vis
		ev.Reminders = remindersJSON(pe.Reminders)
		ev.UpdatedAt = time.Now()
		if err := middleware.ScopedDB(b.app.DB, s.Team.ID).Save(&ev).Error; err != nil {
			return nil, fmt.Errorf("caldav save: %w", err)
		}
	} else {
		ev = models.CalendarEvent{
			ID:           uuid.Must(uuid.NewV7()).String(),
			TeamID:     s.Team.ID,
			UserID:       s.User.ID,
			UID:          pe.UID,
			Title:        pe.Title,
			Location:     pe.Location,
			Description:  pe.Description,
			Category:     pe.Category,
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
		if err := middleware.ScopedDB(b.app.DB, s.Team.ID).Create(&ev).Error; err != nil {
			return nil, fmt.Errorf("caldav create: %w", err)
		}
	}
	return b.toObject(ctx, &ev, calName)
}

// putTodo stores a VTODO written by an external CalDAV client (tasks.org,
// Apple Reminders, ...). Todos are always personal and owned by the caller.
func (b *davBackend) putTodo(ctx context.Context, p, calName string, pt *parsedTodo) (*caldav.CalendarObject, error) {
	s := b.session(ctx)
	if pt.UID == "" || pt.Title == "" {
		return nil, errors.New("无效的 VTODO 数据")
	}
	db := middleware.ScopedDB(b.app.DB, s.Team.ID)
	var t models.Todo
	if err := db.Where("uid = ?", pt.UID).First(&t).Error; err == nil {
		if t.UserID != s.User.ID {
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
		t.UpdatedAt = time.Now()
		if err := middleware.ScopedDB(b.app.DB, s.Team.ID).Save(&t).Error; err != nil {
			return nil, fmt.Errorf("caldav save todo: %w", err)
		}
	} else {
		t = models.Todo{
			ID:          uuid.Must(uuid.NewV7()).String(),
			TeamID:      s.Team.ID,
			UserID:      s.User.ID,
			UID:         pt.UID,
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
		if err := middleware.ScopedDB(b.app.DB, s.Team.ID).Create(&t).Error; err != nil {
			return nil, fmt.Errorf("caldav create todo: %w", err)
		}
	}
	return b.toTodoObject(ctx, &t, calName)
}

func (b *davBackend) DeleteCalendarObject(ctx context.Context, p string) error {
	cal := calNameFromPath(p)
	if cal == "" {
		return nil
	}
	evs, todos, err := b.teamItems(ctx, cal)
	if err != nil {
		return err
	}
	uid := eventUID(p)
	db := middleware.ScopedDB(b.app.DB, b.session(ctx).Team.ID)
	for i := range todos {
		if todoUID(&todos[i]) == uid {
			return db.Where("uid = ?", uid).Delete(&models.Todo{}).Error
		}
	}
	for i := range evs {
		if evs[i].UID == uid {
			return db.Where("uid = ?", uid).Delete(&models.CalendarEvent{}).Error
		}
	}
	return nil
}

// davAuth: Basic Auth = member email + (family calendar_token OR member password).
func (h *Handler) davAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		email, pass, ok := c.Request.BasicAuth()
		if !ok {
			c.Header("WWW-Authenticate", `Basic realm="HomiHub CalDAV"`)
			httpx.UnauthorizedT(c, "calendar_auth_required")
			return
		}
		pass, err := url.QueryUnescape(pass)
		if err != nil {
			httpx.UnauthorizedT(c, "calendar_auth_required")
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
				return
			}
			if err := h.app.DB.Where("id = ?", u.TeamID).First(&fam).Error; err != nil {
				c.Header("WWW-Authenticate", `Basic realm="HomiHub CalDAV"`)
				httpx.UnauthorizedT(c, "calendar_auth_required")
				return
			}
		}
		var user models.User
		if err := h.app.DB.Where("email = ?", email).First(&user).Error; err != nil {
			c.Header("WWW-Authenticate", `Basic realm="HomiHub CalDAV"`)
			httpx.UnauthorizedT(c, "calendar_auth_required")
			return
		}
		var uf models.TeamMember
		if err := h.app.DB.Where("user_id = ? AND team_id = ?", user.ID, fam.ID).First(&uf).Error; err != nil {
			c.Header("WWW-Authenticate", `Basic realm="HomiHub CalDAV"`)
			httpx.UnauthorizedT(c, "calendar_auth_required")
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
	mux := &davMux{caldav: dh, files: files}
	davMethods := []string{"GET", "HEAD", "PUT", "DELETE", "OPTIONS", "POST", "PROPFIND", "PROPPATCH", "REPORT", "COPY", "MOVE", "MKCOL", "LOCK", "UNLOCK"}
	r.Match(davMethods, "/.well-known/caldav", auth, gin.WrapH(dh))
	r.Match(davMethods, "/dav", auth, gin.WrapH(mux))
	r.Match(davMethods, "/dav/*dav", auth, gin.WrapH(mux))
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
