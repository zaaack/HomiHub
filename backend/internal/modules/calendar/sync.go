package modulecalendar

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-webdav/caldav"
	"gorm.io/gorm"

	"homihub/backend/internal/models"
)

// davSyncNS is the DAV: namespace used by sync-collection.
const davSyncNS = "DAV:"

// syncDAVHandler wraps the emersion caldav.Handler adding RFC 6578
// sync-collection REPORT support and the DAV:sync-token property to PROPFIND
// responses for calendar collections.
type syncDAVHandler struct {
	inner *caldav.Handler
	b     *davBackend
}

func (h *syncDAVHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "REPORT" && isSyncCollectionReport(r):
		h.handleSyncCollection(w, r)
	case r.Method == "PROPFIND" && isCalendarCollectionPath(r.URL.Path):
		h.servePropfind(w, r)
	default:
		h.inner.ServeHTTP(w, r)
	}
}

// isSyncCollectionReport peeks at the REPORT body and reports whether it is a
// DAV:sync-collection request (the body is restored for the caller).
func isSyncCollectionReport(r *http.Request) bool {
	if r.Body == nil {
		return false
	}
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	var root struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return false
	}
	return root.XMLName.Space == davSyncNS && root.XMLName.Local == "sync-collection"
}

// isCalendarCollectionPath reports whether p is a calendar collection
// (…/calendars/<name>/), as opposed to the principal or home-set.
func isCalendarCollectionPath(p string) bool {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	return len(parts) == 4 && parts[0] == "dav" && parts[2] == "calendars"
}

type syncCollectionReq struct {
	XMLName   xml.Name `xml:"DAV: sync-collection"`
	SyncToken string   `xml:"DAV: sync-token"`
	SyncLevel string   `xml:"DAV: sync-level"`
	Prop      struct {
		Props []xml.Name `xml:",any"`
	} `xml:"DAV: prop"`
}

// handleSyncCollection implements RFC 6578 §3.2.
func (h *syncDAVHandler) handleSyncCollection(w http.ResponseWriter, r *http.Request) {
	var req syncCollectionReq
	body, _ := io.ReadAll(r.Body)
	if err := xml.Unmarshal(body, &req); err != nil {
		http.Error(w, "caldav: invalid sync-collection body", http.StatusBadRequest)
		return
	}
	s := h.b.session(r.Context())
	cal := calNameFromPath(r.URL.Path)
	if s == nil || cal == "" {
		http.Error(w, "caldav: not found", http.StatusNotFound)
		return
	}
	current, err := syncCurrentSeq(h.b.app.DB, s.Team.ID, cal)
	if err != nil {
		http.Error(w, "caldav: sync error", http.StatusInternalServerError)
		return
	}
	wantEtag := propRequested(req.Prop.Props, "getetag")

	token := strings.TrimSpace(req.SyncToken)
	var (
		hrefs    []string
		etags    map[string]string
		newToken string = strconv.FormatInt(current, 10)
	)
	switch {
	case token == "":
		// Initial sync: return full listing with a fresh token.
		hrefs, etags, err = h.fullListing(r.Context(), cal, wantEtag)
		if err != nil {
			http.Error(w, "caldav: sync error", http.StatusInternalServerError)
			return
		}
	case token == newToken:
		// No changes since token.
	default:
		old, perr := strconv.ParseInt(token, 10, 64)
		if perr != nil || old > current {
			syncValidTokenError(w)
			return
		}
		hrefs, etags, err = syncChanges(h.b.app.DB, s.Team.ID, cal, old, wantEtag)
		if err != nil {
			http.Error(w, "caldav: sync error", http.StatusInternalServerError)
			return
		}
	}

	ms := syncMultiStatus(hrefs, etags, newToken)
	w.Header().Set("Content-Type", "application/xml; charset=\"utf-8\"")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(ms)
}

func syncValidTokenError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/xml; charset=\"utf-8\"")
	w.WriteHeader(http.StatusConflict)
	w.Write([]byte(xml.Header))
	w.Write([]byte(`<d:error xmlns:d="DAV:"><d:valid-sync-token/></d:error>`))
}

// propRequested reports whether the requested props include name (local name).
func propRequested(props []xml.Name, name string) bool {
	for _, p := range props {
		if strings.EqualFold(p.Local, name) {
			return true
		}
	}
	return false
}

// syncCurrentSeq returns the current sync sequence for a calendar.
func syncCurrentSeq(base *gorm.DB, teamID, cal string) (int64, error) {
	var seq int64
	err := base.Session(&gorm.Session{NewDB: true}).Model(&models.CalendarSyncLog{}).
		Where("team_id = ? AND calendar = ?", teamID, cal).
		Select("COALESCE(MAX(seq), 0)").
		Scan(&seq).Error
	return seq, err
}

// syncLogChange appends a mutation to the sync log and returns the new seq.
func syncLogChange(base *gorm.DB, teamID, cal, href, etag string, deleted bool) (int64, error) {
	cur, err := syncCurrentSeq(base, teamID, cal)
	if err != nil {
		return 0, err
	}
	seq := cur + 1
	row := models.CalendarSyncLog{
		TeamID:    teamID,
		Calendar:  cal,
		Seq:       seq,
		Href:      href,
		Etag:      etag,
		Deleted:   deleted,
		CreatedAt: time.Now(),
	}
	if err := base.Session(&gorm.Session{NewDB: true}).Create(&row).Error; err != nil {
		return 0, err
	}
	pruneSyncLog(base, teamID, cal)
	return seq, nil
}

// pruneSyncLog keeps only the most recent 5000 entries per calendar so the
// table does not grow unbounded.
func pruneSyncLog(base *gorm.DB, teamID, cal string) {
	var minID int64
	base.Session(&gorm.Session{NewDB: true}).Model(&models.CalendarSyncLog{}).
		Where("team_id = ? AND calendar = ?", teamID, cal).
		Order("id DESC").Offset(5000).Limit(1).
		Pluck("id", &minID)
	if minID > 0 {
		base.Session(&gorm.Session{NewDB: true}).
			Where("team_id = ? AND calendar = ? AND id <= ?", teamID, cal, minID).
			Delete(&models.CalendarSyncLog{})
	}
}

// syncChanges returns hrefs (and etags when wantEtag) mutated after seq.
// Deleted resources are included as hrefs with a nil etag marker.
type syncChange struct {
	Href    string
	Etag    string
	Deleted bool
}

func syncChanges(base *gorm.DB, teamID, cal string, after int64, wantEtag bool) ([]string, map[string]string, error) {
	var rows []models.CalendarSyncLog
	if err := base.Session(&gorm.Session{NewDB: true}).
		Where("team_id = ? AND calendar = ? AND seq > ?", teamID, cal, after).
		Order("seq").Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	// Dedupe by href keeping the latest row.
	latest := map[string]syncChange{}
	for _, row := range rows {
		latest[row.Href] = syncChange{Href: row.Href, Etag: row.Etag, Deleted: row.Deleted}
	}
	hrefs := make([]string, 0, len(latest))
	etags := map[string]string{}
	for _, ch := range latest {
		hrefs = append(hrefs, ch.Href)
		if wantEtag && !ch.Deleted {
			etags[ch.Href] = ch.Etag
		}
	}
	return hrefs, etags, nil
}

// fullListing returns all current object hrefs in a calendar.
func (h *syncDAVHandler) fullListing(ctx context.Context, cal string, wantEtag bool) ([]string, map[string]string, error) {
	var req caldav.CalendarCompRequest
	if wantEtag {
		req.AllProps = true
	}
	objs, err := h.b.ListCalendarObjects(ctx, calendarPath(h.b.session(ctx).Email, cal), &req)
	if err != nil {
		return nil, nil, err
	}
	hrefs := make([]string, 0, len(objs))
	etags := map[string]string{}
	for i := range objs {
		hrefs = append(hrefs, objs[i].Path)
		if wantEtag {
			etags[objs[i].Path] = objs[i].ETag
		}
	}
	return hrefs, etags, nil
}

// --- multistatus XML building (DAV: namespace) ---

type msResponse struct {
	XMLName  xml.Name    `xml:"DAV: response"`
	Href     string      `xml:"DAV: href"`
	PropStat *msPropStat `xml:"DAV: propstat,omitempty"`
	Status   string      `xml:"DAV: status,omitempty"`
}

type msPropStat struct {
	Prop   msProp `xml:"DAV: prop"`
	Status string `xml:"DAV: status"`
}

type msProp struct {
	GetEtag string `xml:"DAV: getetag,omitempty"`
}

type syncMultiStatusXML struct {
	XMLName   xml.Name     `xml:"DAV: multistatus"`
	Responses []msResponse `xml:"DAV: response"`
	SyncToken string       `xml:"DAV: sync-token"`
}

func syncMultiStatus(hrefs []string, etags map[string]string, token string) *syncMultiStatusXML {
	ms := &syncMultiStatusXML{SyncToken: token}
	for _, href := range hrefs {
		resp := msResponse{Href: href}
		if etag, ok := etags[href]; ok {
			resp.PropStat = &msPropStat{
				Prop:   msProp{GetEtag: etag},
				Status: "HTTP/1.1 200 OK",
			}
		} else {
			resp.Status = "HTTP/1.1 404 Not Found"
		}
		ms.Responses = append(ms.Responses, resp)
	}
	return ms
}

// --- PROPFIND sync-token injection ---

// captureWriter buffers the response so the sync-token property can be
// injected into the collection's propstat before flushing.
type captureWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (c *captureWriter) WriteHeader(code int) {
	c.status = code
}

func (c *captureWriter) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(p)
}

// servePropfind runs the inner PROPFIND handler, then injects the
// DAV:sync-token property into the calendar collection's response element.
func (h *syncDAVHandler) servePropfind(w http.ResponseWriter, r *http.Request) {
	cw := &captureWriter{ResponseWriter: w}
	h.inner.ServeHTTP(cw, r)
	if cw.status == 0 {
		cw.status = http.StatusOK
	}
	token := ""
	if s := h.b.session(r.Context()); s != nil {
		if cal := calNameFromPath(r.URL.Path); cal != "" {
			if seq, err := syncCurrentSeq(h.b.app.DB, s.Team.ID, cal); err == nil {
				token = strconv.FormatInt(seq, 10)
			}
		}
	}
	out := injectSyncTokenProp(cw.body.Bytes(), token)
	w.WriteHeader(cw.status)
	w.Write(out)
}

// frame captures the tokens of a <propstat> element while parsing a PROPFIND
// response so sync-token can be surgically removed without touching siblings.
type syncFrame struct {
	start  xml.StartElement
	inside []xml.Token
}

// copyToken deep-copies a decoded token. encoding/xml reuses its internal
// buffer between Token calls, so byte slices must be duplicated before they
// are buffered. Namespace declaration attributes are dropped: the encoder
// regenerates them from each element's resolved Name.Space, otherwise the
// output would carry duplicate xmlns declarations.
func copyToken(t xml.Token) xml.Token {
	c := xml.CopyToken(t)
	if se, ok := c.(xml.StartElement); ok {
		attrs := se.Attr[:0]
		for _, a := range se.Attr {
			if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
				continue
			}
			attrs = append(attrs, a)
		}
		se.Attr = attrs
		return se
	}
	return c
}

// injectSyncTokenProp adds a DAV:sync-token 200 propstat to the calendar
// collection's response element (the first <response>, which never nests). The
// library reports the property as 404; that sync-token element is dropped so
// clients see a single, authoritative 200 value while sibling props are kept.
func injectSyncTokenProp(body []byte, token string) []byte {
	if token == "" {
		return body
	}
	var stack []syncFrame
	var out []xml.Token
	inColl := false
	seenResp := false

	flush := func() {
		for _, f := range stack {
			out = append(out, f.start)
			out = append(out, f.inside...)
			out = append(out, xml.EndElement{Name: f.start.Name})
		}
		stack = nil
	}

	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return body
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !seenResp && t.Name.Local == "response" {
				seenResp = true
				inColl = true
			}
			if inColl && t.Name.Local == "propstat" {
				stack = append(stack, syncFrame{start: copyToken(t).(xml.StartElement)})
			} else if len(stack) > 0 {
				stack[len(stack)-1].inside = append(stack[len(stack)-1].inside, copyToken(tok))
			} else {
				out = append(out, copyToken(tok))
			}
		case xml.EndElement:
			if inColl && t.Name.Local == "propstat" {
				f := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if !propstatHasSyncToken(f) {
					out = append(out, f.start)
					out = append(out, f.inside...)
					out = append(out, t)
				}
			} else if len(stack) > 0 {
				stack[len(stack)-1].inside = append(stack[len(stack)-1].inside, copyToken(tok))
			} else if inColl && t.Name.Local == "response" {
				// End of the collection response: emit the sync-token propstat.
				out = append(out, syncTokenPropstat(token)...)
				out = append(out, t)
				inColl = false
			} else {
				out = append(out, copyToken(tok))
			}
		default:
			if len(stack) > 0 {
				stack[len(stack)-1].inside = append(stack[len(stack)-1].inside, copyToken(tok))
			} else {
				out = append(out, copyToken(tok))
			}
		}
	}
	flush()
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	for _, tok := range out {
		if err := enc.EncodeToken(tok); err != nil {
			return body
		}
	}
	if err := enc.Flush(); err != nil {
		return body
	}
	return buf.Bytes()
}

// propstatHasSyncToken reports whether a captured propstat contains a
// sync-token property element.
func propstatHasSyncToken(f syncFrame) bool {
	for _, tok := range f.inside {
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "sync-token" {
			return true
		}
	}
	return false
}

// syncTokenPropstat builds a 200 OK propstat carrying the sync-token value.
func syncTokenPropstat(token string) []xml.Token {
	ns := xml.Name{Space: davSyncNS, Local: "sync-token"}
	prop := xml.StartElement{Name: xml.Name{Space: davSyncNS, Local: "prop"}}
	st := xml.StartElement{Name: ns}
	se := xml.EndElement{Name: ns}
	pe := xml.EndElement{Name: prop.Name}
	status := xml.StartElement{Name: xml.Name{Space: davSyncNS, Local: "status"}}
	seStatus := xml.EndElement{Name: status.Name}
	return []xml.Token{
		xml.StartElement{Name: xml.Name{Space: davSyncNS, Local: "propstat"}},
		prop,
		st,
		xml.CharData(token),
		se,
		pe,
		status,
		xml.CharData("HTTP/1.1 200 OK"),
		seStatus,
		xml.EndElement{Name: xml.Name{Space: davSyncNS, Local: "propstat"}},
	}
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

func middlewareScopedDB(db *gorm.DB, teamID string) *gorm.DB {
	return db.Session(&gorm.Session{NewDB: true}).Where("team_id = ?", teamID)
}

func timeNow() time.Time {
	return time.Now()
}
