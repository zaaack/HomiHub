package modulecalendar

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"homihub/backend/internal/models"
)

// caldavScheduleNS is the CalDAV namespace used by schedule-response XML.
const caldavScheduleNS = "urn:ietf:params:xml:ns:caldav"

// scheduleMailboxPath splits a schedule mailbox path. It returns the mailbox
// kind ("inbox" or "outbox") and, for a message URL, the message href filename.
func scheduleMailboxPath(p string) (kind string, item string) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) < 3 || parts[0] != "dav" {
		return "", ""
	}
	if parts[2] != "inbox" && parts[2] != "outbox" {
		return "", ""
	}
	kind = parts[2]
	if len(parts) >= 4 {
		item = parts[3]
	}
	return kind, item
}

// scheduleInboxPath returns the schedule-inbox collection path for an email.
func scheduleInboxPath(email string) string {
	return "/dav/" + url.PathEscape(email) + "/inbox/"
}

// scheduleInboxMessagePath returns the schedule-inbox message path for an email.
func scheduleInboxMessagePath(email, item string) string {
	return "/dav/" + url.PathEscape(email) + "/inbox/" + item
}

// handleScheduleOutboxPost handles a POST to the schedule-outbox (RFC 6638 §4.1):
// a VFREEBUSY REQUEST is answered with a schedule-response XML document.
func (h *syncDAVHandler) handleScheduleOutboxPost(w http.ResponseWriter, r *http.Request) {
	// Team-scoped accounts have no personal identity to schedule on behalf of.
	if s := h.b.session(r.Context()); s == nil || s.IsTeam() {
		http.Error(w, "caldav: forbidden", http.StatusForbidden)
		return
	}
	cal, err := parseCalendar(r)
	if err != nil {
		http.Error(w, "caldav: invalid iCalendar body", http.StatusBadRequest)
		return
	}
	resp := h.buildFreeBusyResponse(r.Context(), cal)
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(resp)
}

// parseCalendar decodes the request body into an ical.Calendar.
func parseCalendar(r *http.Request) (*ical.Calendar, error) {
	return ical.NewDecoder(r.Body).Decode()
}

// freeBusyRange extracts the queried time range and attendee addresses from a
// VFREEBUSY REQUEST calendar.
func freeBusyRange(cal *ical.Calendar) (start, end time.Time, attendees []string, ok bool) {
	var fb *ical.Component
	for _, c := range cal.Children {
		if c.Name == ical.CompFreeBusy {
			fb = c
			break
		}
	}
	if fb == nil {
		return start, end, nil, false
	}
	start, err := fb.Props.DateTime(ical.PropDateTimeStart, nil)
	if err != nil {
		return start, end, nil, false
	}
	end, err = fb.Props.DateTime(ical.PropDateTimeEnd, nil)
	if err != nil {
		return start, end, nil, false
	}
	for _, p := range fb.Props.Values(ical.PropAttendee) {
		addr := strings.TrimSpace(p.Value)
		if addr != "" {
			attendees = append(attendees, addr)
		}
	}
	if len(attendees) == 0 {
		return start, end, nil, false
	}
	return start.UTC(), end.UTC(), attendees, true
}

// normalizeAddress strips scheme/relative prefixes from a calendar address
// ("mailto:foo@bar", "./mailto:foo@bar", "MAILTO:foo@bar") down to the bare
// email, which is what the users table stores.
func normalizeAddress(addr string) string {
	a := strings.TrimSpace(addr)
	a = strings.TrimPrefix(a, "./")
	a = strings.TrimPrefix(a, "mailto:")
	a = strings.TrimPrefix(a, "MAILTO:")
	a = strings.TrimPrefix(a, "./")
	return strings.TrimSpace(a)
}

// buildFreeBusyResponse computes the busy periods for each attendee and wraps
// them in a schedule-response document the way python-caldav expects.
func (h *syncDAVHandler) buildFreeBusyResponse(ctx context.Context, cal *ical.Calendar) *scheduleResponseXML {
	start, end, attendees, ok := freeBusyRange(cal)
	resp := &scheduleResponseXML{}
	if !ok {
		resp.Responses = append(resp.Responses, scheduleResponseXMLItem{
			Recipient:     "",
			RequestStatus: "3.7;Invalid Calendar Object",
		})
		return resp
	}
	for _, addr := range attendees {
		item := scheduleResponseXMLItem{
			Recipient:     addr,
			RequestStatus: "2.0;Success",
		}
		email := normalizeAddress(addr)
		var user models.User
		err := h.b.app.DB.Session(&gorm.Session{NewDB: true}).
			Where("email = ?", email).First(&user).Error
		if err != nil {
			item.RequestStatus = "3.7;Invalid Calendar User"
			resp.Responses = append(resp.Responses, item)
			continue
		}
		item.CalendarData = buildFreeBusyICS(start, end, busyPeriods(h.b.app.DB, user, start, end))
		resp.Responses = append(resp.Responses, item)
	}
	return resp
}

// busyPeriods returns the attendee's busy intervals overlapping [start, end]
// from every calendar they can see in their team.
func busyPeriods(db *gorm.DB, user models.User, start, end time.Time) [][2]time.Time {
	var evs []models.CalendarEvent
	middlewareScopedDB(db, user.TeamID).
		Where("component_type = ? AND starts_at < ? AND ends_at > ?", ical.CompEvent, end, start).
		Find(&evs)
	periods := [][2]time.Time{}
	for _, ev := range evs {
		if ev.UserID != user.ID && ev.Visibility == models.VisibilityPrivate {
			continue
		}
		periods = append(periods, [2]time.Time{ev.StartsAt, ev.EndsAt})
	}
	return periods
}

// buildFreeBusyICS renders a VFREEBUSY iCalendar with the given busy periods.
func buildFreeBusyICS(start, end time.Time, periods [][2]time.Time) string {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//HomiHub//FreeBusy//CN")
	cal.Props.SetText(ical.PropMethod, "REPLY")
	fb := ical.NewComponent(ical.CompFreeBusy)
	fb.Props.SetText(ical.PropUID, uuid.Must(uuid.NewV7()).String())
	fb.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	fb.Props.SetDateTime(ical.PropDateTimeStart, start)
	fb.Props.SetDateTime(ical.PropDateTimeEnd, end)
	for _, p := range periods {
		prop := ical.NewProp("FREEBUSY")
		prop.Value = p[0].UTC().Format("20060102T150405Z") + "/" + p[1].UTC().Format("20060102T150405Z")
		fb.Props.Set(prop)
	}
	cal.Children = append(cal.Children, fb)
	return string(serialize(cal))
}

// --- schedule-inbox handling ---

// handleScheduleInboxPropfind lists the schedule-inbox messages for the
// authenticated user. It answers the PROPFIND depth-1 fallback used by
// python-caldav's ScheduleMailbox.get_items.
func (h *syncDAVHandler) handleScheduleInboxPropfind(w http.ResponseWriter, r *http.Request) {
	s := h.b.session(r.Context())
	if s == nil {
		http.Error(w, "caldav: unauthorized", http.StatusUnauthorized)
		return
	}
	if s.IsTeam() {
		http.Error(w, "caldav: forbidden", http.StatusForbidden)
		return
	}
	var msgs []models.ScheduleMessage
	middlewareScopedDB(h.b.app.DB, s.Team.ID).Where("user_id = ?", s.User.ID).Find(&msgs)
	body := inboxMultiStatus{}
	// Collection response first.
	coll := inboxMSResponse{Href: scheduleInboxPath(s.Email)}
	coll.PropStat.Status = "HTTP/1.1 200 OK"
	coll.PropStat.Prop.DisplayName = "Schedule Inbox"
	coll.PropStat.Prop.ResourceType.Collection = &xml.Name{Space: "DAV:", Local: "collection"}
	body.Responses = append(body.Responses, coll)
	for _, m := range msgs {
		item := inboxMSResponse{Href: scheduleInboxMessagePath(s.Email, path.Base(m.ID)+".ics")}
		item.PropStat.Status = "HTTP/1.1 200 OK"
		item.PropStat.Prop.DisplayName = m.UID
		body.Responses = append(body.Responses, item)
	}
	w.Header().Set("Content-Type", "application/xml; charset=\"utf-8\"")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(body)
}

type inboxMultiStatus struct {
	XMLName   xml.Name          `xml:"DAV: multistatus"`
	Responses []inboxMSResponse `xml:"DAV: response"`
}

type inboxMSResponse struct {
	Href     string          `xml:"DAV: href"`
	PropStat inboxMSPropStat `xml:"DAV: propstat"`
}

type inboxMSPropStat struct {
	Status string       `xml:"DAV: status"`
	Prop   inboxMSProps `xml:"DAV: prop"`
}

type inboxMSProps struct {
	DisplayName  string         `xml:"DAV: displayname"`
	ResourceType inboxMSResType `xml:"DAV: resourcetype"`
}

type inboxMSResType struct {
	Collection *xml.Name `xml:"DAV: collection,omitempty"`
}

// handleScheduleInboxGet returns a single inbox message's iTIP payload.
func (h *syncDAVHandler) handleScheduleInboxGet(w http.ResponseWriter, r *http.Request) {
	s := h.b.session(r.Context())
	if s == nil {
		http.Error(w, "caldav: unauthorized", http.StatusUnauthorized)
		return
	}
	if s.IsTeam() {
		http.Error(w, "caldav: forbidden", http.StatusForbidden)
		return
	}
	_, item := scheduleMailboxPath(r.URL.Path)
	msg, err := inboxMessage(h.b.app.DB, s, item)
	if err != nil {
		http.Error(w, "caldav: not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", ical.MIMEType)
	w.Write([]byte(msg.Ics))
}

// handleScheduleInboxDelete removes an inbox message (RFC 6638 §4.2).
func (h *syncDAVHandler) handleScheduleInboxDelete(w http.ResponseWriter, r *http.Request) {
	s := h.b.session(r.Context())
	if s == nil {
		http.Error(w, "caldav: unauthorized", http.StatusUnauthorized)
		return
	}
	if s.IsTeam() {
		http.Error(w, "caldav: forbidden", http.StatusForbidden)
		return
	}
	_, item := scheduleMailboxPath(r.URL.Path)
	msg, err := inboxMessage(h.b.app.DB, s, item)
	if err != nil {
		http.Error(w, "caldav: not found", http.StatusNotFound)
		return
	}
	if err := middlewareScopedDB(h.b.app.DB, s.Team.ID).Delete(&msg).Error; err != nil {
		http.Error(w, "caldav: delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// inboxMessage resolves an inbox message by its href filename (message ID).
func inboxMessage(db *gorm.DB, s *DavSession, item string) (models.ScheduleMessage, error) {
	var msg models.ScheduleMessage
	id := strings.TrimSuffix(item, ".ics")
	err := middlewareScopedDB(db, s.Team.ID).
		Where("id = ? AND user_id = ?", id, s.User.ID).First(&msg).Error
	return msg, err
}

// --- delivery & auto-schedule ---

// deliverToInboxAndAutoSchedule is invoked after a successful PUT of an event
// with attendees. It delivers an iTIP REQUEST to each attendee's schedule-inbox
// and, since this server auto-schedules, stores a copy in the attendee's
// personal calendar.
func (b *davBackend) deliverToInboxAndAutoSchedule(ctx context.Context, cal *ical.Calendar) {
	s := b.session(ctx)
	if s == nil {
		return
	}
	var organizer string
	for _, ev := range cal.Events() {
		if p := ev.Props.Get(ical.PropOrganizer); p != nil {
			organizer = normalizeAddress(p.Value)
			break
		}
	}
	seen := map[string]bool{}
	for _, ev := range cal.Events() {
		for _, p := range ev.Props.Values(ical.PropAttendee) {
			email := normalizeAddress(p.Value)
			if email == "" || seen[email] {
				continue
			}
			seen[email] = true
			if email == organizer {
				continue
			}
			var attendee models.User
			err := b.app.DB.Session(&gorm.Session{NewDB: true}).
				Where("email = ?", email).First(&attendee).Error
			if err != nil {
				continue
			}
			b.deliverInboxMessage(ctx, cal, &attendee)
			b.autoScheduleEvent(ctx, cal, &attendee)
		}
	}
}

// deliverInboxMessage stores a METHOD:REQUEST iTIP copy in the attendee's inbox.
func (b *davBackend) deliverInboxMessage(ctx context.Context, cal *ical.Calendar, attendee *models.User) {
	ev := firstEvent(cal)
	if ev == nil {
		return
	}
	uid, _ := ev.Props.Text(ical.PropUID)
	if uid == "" {
		return
	}
	msg := ical.NewCalendar()
	msg.Props.SetText(ical.PropVersion, "2.0")
	msg.Props.SetText(ical.PropProductID, "-//HomiHub//iTIP//CN")
	msg.Props.SetText(ical.PropMethod, "REQUEST")
	msg.Children = append(msg.Children, ev)
	ics := string(serialize(msg))
	db := b.app.DB.Session(&gorm.Session{NewDB: true})
	var existing models.ScheduleMessage
	if err := db.Where("team_id = ? AND user_id = ? AND uid = ?", attendee.TeamID, attendee.ID, uid).First(&existing).Error; err == nil {
		existing.Ics = ics
		db.Save(&existing)
		return
	}
	row := models.ScheduleMessage{
		ID:     uuid.Must(uuid.NewV7()).String(),
		UserID: attendee.ID,
		TeamID: attendee.TeamID,
		UID:    uid,
		Ics:    ics,
		Method: "REQUEST",
	}
	if err := db.Create(&row).Error; err != nil {
		return
	}
}

// autoScheduleEvent copies the event (master + exceptions) into the attendee's
// personal calendar as a private event.
func (b *davBackend) autoScheduleEvent(ctx context.Context, cal *ical.Calendar, attendee *models.User) {
	ev := firstEvent(cal)
	if ev == nil {
		return
	}
	uid, _ := ev.Props.Text(ical.PropUID)
	if uid == "" {
		return
	}
	db := b.app.DB.Session(&gorm.Session{NewDB: true})
	var existing models.CalendarEvent
	if err := db.Where("team_id = ? AND user_id = ? AND calendar = ? AND uid = ? AND recurrence_id IS NULL", attendee.TeamID, attendee.ID, calSelf, uid).First(&existing).Error; err == nil {
		return
	}
	start, err1 := ev.Props.DateTime(ical.PropDateTimeStart, nil)
	end, err2 := ev.Props.DateTime(ical.PropDateTimeEnd, nil)
	if err1 != nil || err2 != nil {
		return
	}
	title, _ := ev.Props.Text(ical.PropSummary)
	loc, _ := ev.Props.Text(ical.PropLocation)
	desc, _ := ev.Props.Text(ical.PropDescription)
	row := models.CalendarEvent{
		ID:            uuid.Must(uuid.NewV7()).String(),
		TeamID:        attendee.TeamID,
		UserID:        attendee.ID,
		UID:           uid,
		Calendar:      calSelf,
		ComponentType: ical.CompEvent,
		Title:         title,
		Location:      loc,
		Description:   desc,
		StartsAt:      start.UTC(),
		EndsAt:        end.UTC(),
		Visibility:    models.VisibilityPrivate,
	}
	if err := db.Create(&row).Error; err != nil {
		return
	}
}

// firstEvent returns the first VEVENT component from a calendar, if any.
func firstEvent(cal *ical.Calendar) *ical.Component {
	for _, c := range cal.Children {
		if c.Name == ical.CompEvent {
			return c
		}
	}
	return nil
}

// --- schedule-response XML types ---

type scheduleResponseXML struct {
	XMLName   xml.Name                 `xml:"urn:ietf:params:xml:ns:caldav schedule-response"`
	Responses []scheduleResponseXMLItem `xml:"response"`
}

type scheduleResponseXMLItem struct {
	Recipient     string `xml:"recipient"`
	RequestStatus string `xml:"request-status"`
	CalendarData  string `xml:"calendar-data,omitempty"`
}