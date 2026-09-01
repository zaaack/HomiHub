package modulecalendar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	return "/dav/calendars/" + url.PathEscape(email) + "/" + cal + "/"
}

func principalPath(email string) string {
	return "/dav/principals/" + url.PathEscape(email) + "/"
}

func calNameFromPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) >= 3 && parts[0] == "dav" && parts[1] == "calendars" {
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
	return "/dav/calendars/" + url.PathEscape(b.session(ctx).Email) + "/", nil
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
			SupportedComponentSet: []string{ical.CompEvent},
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
		return &caldav.Calendar{Path: p, Name: "我的", Description: "我的私人日历", SupportedComponentSet: []string{ical.CompEvent}}, nil
	}
	if cal == calTeam {
		return &caldav.Calendar{Path: p, Name: s.Team.Name, Description: s.Team.Name + " 的共享日历", SupportedComponentSet: []string{ical.CompEvent}}, nil
	}
	return nil, errNotFound
}

// teamEvents returns the events visible in a calendar:
//   - self:   the member's Private events + their dated personal todos
//   - family: events with visibility Family or Busy
func (b *davBackend) teamEvents(ctx context.Context, cal string) ([]models.CalendarEvent, error) {
	s := b.session(ctx)
	db := middleware.ScopedDB(b.app.DB, s.Team.ID)
	var evs []models.CalendarEvent
	switch {
	case cal == calSelf:
		if err := db.Where("user_id = ? AND visibility = ?", s.User.ID, models.VisibilityPrivate).
			Order("starts_at").Find(&evs).Error; err != nil {
			return nil, err
		}
		var todos []models.Todo
		if err := db.Where("user_id = ? AND due_at IS NOT NULL", s.User.ID).
			Order("due_at").Find(&todos).Error; err == nil {
			for i := range todos {
				t := &todos[i]
				end := t.DueAt.Add(time.Hour)
				evs = append(evs, models.CalendarEvent{
					ID: "todo-" + t.ID, UserID: s.User.ID, Title: t.Title,
					Description: t.Note, StartsAt: *t.DueAt, EndsAt: end,
					UID: "todo-" + t.ID, RRule: t.RRule,
					Visibility: models.VisibilityPrivate, Category: "family",
					CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
				})
			}
		}
	case cal == calTeam:
		if err := db.Where("visibility IN ?", []int{models.VisibilityTeam, models.VisibilityBusy}).
			Order("starts_at").Find(&evs).Error; err != nil {
			return nil, err
		}
	default:
		return nil, nil
	}
	return evs, nil
}

func eventUID(p string) string {
	base := path.Base(p)
	return strings.TrimSuffix(base, ".ics")
}

func eventEtag(ev *models.CalendarEvent) string {
	sum := sha256.Sum256([]byte(ev.UID + ev.StartsAt.String() + ev.RRule + ev.Title + ev.UpdatedAt.String()))
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

func (b *davBackend) GetCalendarObject(ctx context.Context, p string, req *caldav.CalendarCompRequest) (*caldav.CalendarObject, error) {
	cal := calNameFromPath(p)
	if cal == "" {
		return nil, errNotFound
	}
	evs, err := b.teamEvents(ctx, cal)
	if err != nil {
		return nil, err
	}
	uid := eventUID(p)
	for i := range evs {
		if evs[i].UID == uid {
			return b.toObject(ctx, &evs[i], cal)
		}
	}
	return nil, errNotFound
}

func (b *davBackend) ListCalendarObjects(ctx context.Context, p string, req *caldav.CalendarCompRequest) ([]caldav.CalendarObject, error) {
	cal := calNameFromPath(p)
	if cal == "" {
		return nil, nil
	}
	evs, err := b.teamEvents(ctx, cal)
	if err != nil {
		return nil, err
	}
	out := make([]caldav.CalendarObject, 0, len(evs))
	for i := range evs {
		obj, err := b.toObject(ctx, &evs[i], cal)
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
	evs, err := b.teamEvents(ctx, cal)
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
	return out, nil
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
// Events PUT to the self calendar are forced VisibilityPrivate.
func (b *davBackend) PutCalendarObject(ctx context.Context, p string, cal *ical.Calendar, opts *caldav.PutCalendarObjectOptions) (*caldav.CalendarObject, error) {
	s := b.session(ctx)
	calName := calNameFromPath(p)
	parsed := ParseEvents(cal)
	if len(parsed) == 0 {
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
		ev.RecurrenceID = pe.RecurrenceID
		ev.Visibility = vis
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
			RecurrenceID: pe.RecurrenceID,
			Visibility:   vis,
		}
		if err := middleware.ScopedDB(b.app.DB, s.Team.ID).Create(&ev).Error; err != nil {
			return nil, fmt.Errorf("caldav create: %w", err)
		}
	}
	return b.toObject(ctx, &ev, calName)
}

func (b *davBackend) DeleteCalendarObject(ctx context.Context, p string) error {
	cal := calNameFromPath(p)
	if cal == "" {
		return nil
	}
	evs, err := b.teamEvents(ctx, cal)
	if err != nil {
		return err
	}
	uid := eventUID(p)
	found := false
	for i := range evs {
		if evs[i].UID == uid {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	return middleware.ScopedDB(b.app.DB, b.session(ctx).Team.ID).
		Where("uid = ?", uid).Delete(&models.CalendarEvent{}).Error
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
	davMethods := []string{"GET", "HEAD", "PUT", "DELETE", "OPTIONS", "POST", "PROPFIND", "REPORT", "COPY", "MOVE", "MKCOL", "LOCK", "UNLOCK"}
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
