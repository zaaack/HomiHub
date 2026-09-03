package models

import (
	"encoding/json"
	"strings"
	"time"
)

// Visibility constants for calendar events.
const (
	VisibilityPrivate = 1 // Only owner
	VisibilityBusy    = 2 // Others see busy, no details
	VisibilityTeam    = 3 // Everyone (default)
)

// Scopes for the file library.
const (
	ScopePublic   = "public"
	ScopePersonal = "personal"
)

type Team struct {
	ID            string    `gorm:"primaryKey;size:36" json:"id"`
	Name          string    `gorm:"size:64;not null" json:"name"`
	OwnerID       string    `gorm:"size:36" json:"ownerId"`
	CalendarToken string    `gorm:"size:64;uniqueIndex;not null" json:"-"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type User struct {
	ID           string    `gorm:"primaryKey;size:36" json:"id"`
	TeamID       string    `gorm:"size:36;index" json:"teamId"`
	Email        string    `gorm:"size:255;uniqueIndex;not null" json:"email"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Name         string    `gorm:"size:64;not null" json:"name"`
	Role         string    `gorm:"size:16;default:child" json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// TeamMember is the multi-space membership join table.
type TeamMember struct {
	UserID string `gorm:"primaryKey;size:36" json:"userId"`
	TeamID string `gorm:"primaryKey;size:36" json:"teamId"`
	Role   string `gorm:"size:16;default:child" json:"role"`
}

type Token struct {
	ID         string     `gorm:"primaryKey;size:36" json:"id"`
	TeamID     string     `gorm:"size:36;index" json:"teamId"`
	Kind       string     `gorm:"size:16;not null" json:"kind"`
	SubjectID  string     `gorm:"size:36" json:"subjectId"`
	Name       string     `gorm:"size:128" json:"name"`
	TokenHash  string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	// TokenValue keeps the raw value of app passwords (kind="app_password") so
	// they can be re-displayed/copied later. Session tokens leave it empty.
	TokenValue string     `gorm:"size:128" json:"-"`
	IP         string     `gorm:"size:64" json:"ip"`
	UserAgent  string     `gorm:"size:512" json:"userAgent"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type Invite struct {
	ID        string     `gorm:"primaryKey;size:36" json:"id"`
	TeamID    string     `gorm:"size:36;index" json:"teamId"`
	TokenHash string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	Role      string     `gorm:"size:16;default:child" json:"role"`
	ExpiresAt time.Time  `json:"expiresAt"`
	UsedAt    *time.Time `json:"usedAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

// Calendar stores per-user/team calendar metadata for MKCALENDAR support and
// the todo lists created from the web UI (a list is a VTODO-only calendar).
type Calendar struct {
	ID          string    `gorm:"primaryKey;size:36" json:"id"`
	TeamID      string    `gorm:"size:36;index:idx_cal_name,unique;index" json:"teamId"`
	Name        string    `gorm:"size:64;index:idx_cal_name,unique" json:"name"` // path segment, unique per team
	DisplayName string    `gorm:"size:255" json:"displayName"`
	Description string    `gorm:"size:2000" json:"description"`
	Color       string    `gorm:"size:32" json:"color"`
	Icon        string    `gorm:"size:64" json:"icon"`
	Components  string    `gorm:"size:128" json:"components"` // "VEVENT,VTODO,VJOURNAL" etc.
	Access      string    `gorm:"size:16;default:legacy" json:"access"` // ""(legacy, whole team) or CalendarAccessMembers
	OwnerID     string    `gorm:"size:36" json:"ownerId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Calendar access modes.
const (
	// CalendarAccessLegacy marks calendars created outside the todo-list UI
	// (MKCALENDAR etc.): VTODO rows there stay visible to the whole team, as
	// before per-member sharing existed.
	CalendarAccessLegacy = "legacy"
	// CalendarAccessMembers marks todo lists created from the web UI: only the
	// owner and the users in CalendarShare rows can see / write them.
	CalendarAccessMembers = "members"
)

// CalendarShare grants a team member access to a member-scoped todo list (a
// models.Calendar row with Access = CalendarAccessMembers, Components = "VTODO").
// The owner always keeps access even without a row.
type CalendarShare struct {
	TeamID    string    `gorm:"size:36;primaryKey" json:"-"`
	Calendar  string    `gorm:"size:64;primaryKey" json:"calendar"`
	UserID    string    `gorm:"size:36;primaryKey" json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
}

type CalendarEvent struct {
	ID            string     `gorm:"primaryKey;size:36" json:"id"`
	TeamID        string     `gorm:"size:36;index" json:"teamId"`
	UserID        string     `gorm:"size:36;index" json:"userId"`
	UID           string     `gorm:"size:64;index" json:"uid"`
	Calendar      string     `gorm:"size:64;default:team;index" json:"calendar"`  // calendar name (self, team, or custom)
	ComponentType string     `gorm:"size:16;default:VEVENT" json:"componentType"` // VEVENT or VJOURNAL
	Title         string     `gorm:"size:255;not null" json:"title"`
	Category      string     `gorm:"size:16;not null" json:"category"`
	Location      string     `gorm:"size:255" json:"location"`
	Description   string     `gorm:"size:2000" json:"description"`
	Class         string     `gorm:"size:32" json:"-"`
	Duration      string     `gorm:"size:64" json:"-"`
	StartsAt      time.Time  `json:"startsAt"`
	EndsAt        time.Time  `json:"endsAt"`
	AllDay        bool       `json:"allDay"`
	RRule         string     `gorm:"size:255" json:"rrule"`
	ExDate        string     `gorm:"type:text" json:"-"`
	RecurrenceID  *time.Time `json:"recurrenceId"`
	RelatedTo     string     `gorm:"size:255" json:"relatedTo"`
	Visibility    int        `gorm:"default:3" json:"visibility"`
	Attendees     string     `gorm:"type:text" json:"-"`
	Reminders     string     `gorm:"type:text" json:"-"`
	Tags          string     `gorm:"size:500" json:"tags"` // VJOURNAL notes: comma-separated tags (CATEGORIES)
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// ScheduleMessage is a raw iTIP message delivered to a user's schedule-inbox
// (RFC 6638). Items are transient: clients process and delete them.
type ScheduleMessage struct {
	ID        string    `gorm:"primaryKey;size:36" json:"id"`
	UserID    string    `gorm:"size:36;index" json:"userId"`
	TeamID    string    `gorm:"size:36;index" json:"teamId"`
	UID       string    `gorm:"size:64;index" json:"uid"`
	Ics       string    `gorm:"type:text" json:"-"`
	Method    string    `gorm:"size:16" json:"method"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Setting is a global key-value store (not tenant scoped).
type Setting struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"size:4096" json:"value"`
}

// Reminder is a pre-notification, either relative to the event/todo (Unit =
// min|hour|day + Value) or at an absolute time (Unit = "at", At set), stored as
// a JSON array on the model and serialized to VALARM in iCal.
type Reminder struct {
	Unit  string     `json:"unit"` // min|hour|day|at
	Value int        `json:"value"`
	At    *time.Time `json:"at,omitempty"`
}

// Attendance statuses (RFC 5545 PARTSTAT).
const (
	AttendeeStatusNeedsAction = "NEEDS-ACTION"
	AttendeeStatusAccepted    = "ACCEPTED"
	AttendeeStatusDeclined    = "DECLINED"
)

// Attendee is a team member invited to an event or a todo. It mirrors the RFC
// 5545 ATTENDEE property and is stored as a JSON array on the model.
type Attendee struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Status string `json:"status"` // NEEDS-ACTION | ACCEPTED | DECLINED
}

// ParseAttendees decodes the stored JSON column into an attendee slice.
func ParseAttendees(s string) []Attendee {
	if s == "" {
		return nil
	}
	var out []Attendee
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	for i := range out {
		if out[i].Status == "" {
			out[i].Status = AttendeeStatusNeedsAction
		}
	}
	return out
}

// AttendeesJSON encodes an attendee slice for storage.
func AttendeesJSON(as []Attendee) string {
	if len(as) == 0 {
		return ""
	}
	b, err := json.Marshal(as)
	if err != nil {
		return ""
	}
	return string(b)
}

// CalendarSyncLog tracks calendar mutations for RFC 6578 sync-collection.
type CalendarSyncLog struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	TeamID    string `gorm:"size:36;index:idx_cal_sync,priority:1"`
	Calendar  string `gorm:"size:64;index:idx_cal_sync,priority:2"` // "self", "team", or custom calendar name
	Seq       int64
	Href      string `gorm:"size:512"`
	Etag      string `gorm:"size:64"`
	Deleted   bool
	CreatedAt time.Time
}

// Seconds returns the duration this relative reminder represents (0 for "at").
func (r Reminder) Seconds() int {
	switch r.Unit {
	case "at":
		return 0
	case "hour":
		return r.Value * 3600
	case "day":
		return r.Value * 86400
	default:
		return r.Value * 60
	}
}

// ParseReminders decodes the stored JSON column into a reminder slice.
func ParseReminders(s string) []Reminder {
	if s == "" {
		return nil
	}
	var out []Reminder
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// RemindersJSON encodes a reminder slice for storage.
func RemindersJSON(rs []Reminder) string {
	if len(rs) == 0 {
		return ""
	}
	b, err := json.Marshal(rs)
	if err != nil {
		return ""
	}
	return string(b)
}

// ExDateJoin renders exception dates as a comma-separated RFC3339 string for
// the model's ExDate column.
func ExDateJoin(dates []time.Time) string {
	parts := make([]string, 0, len(dates))
	for _, d := range dates {
		parts = append(parts, d.UTC().Format(time.RFC3339))
	}
	return strings.Join(parts, ",")
}

// ExDateSplit parses the model's ExDate column back into UTC times.
func ExDateSplit(s string) []time.Time {
	if s == "" {
		return nil
	}
	var out []time.Time
	for _, p := range strings.Split(s, ",") {
		if p == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, p); err == nil {
			out = append(out, t.UTC())
		}
	}
	return out
}

// Todo is a task that surfaces in a CalDAV calendar as VTODO: personal todos
// in the self calendar, team-shared todos in the team calendar, and todos of
// a todo list in its VTODO-only custom calendar (Calendar.Access members).
type Todo struct {
	ID          string     `gorm:"primaryKey;size:36" json:"id"`
	TeamID      string     `gorm:"size:36;index" json:"teamId"`
	UserID      string     `gorm:"size:36;index" json:"userId"`
	UID         string     `gorm:"size:64;index" json:"uid"`
	Title       string     `gorm:"size:255;not null" json:"title"`
	Note        string     `gorm:"size:2000" json:"note"`
	Completed   bool       `json:"completed"`
	Calendar    string     `gorm:"size:64;default:self;index" json:"calendar"` // "self", "team", or a custom list calendar name
	Shared      bool       `gorm:"default:false" json:"shared"`
	DueAt       *time.Time `json:"dueAt"`
	RRule       string     `gorm:"size:255" json:"rrule"`
	ExDate      string     `gorm:"type:text" json:"-"`
	Group       string     `gorm:"size:64;index" json:"group"`
	Tags        string     `gorm:"size:500" json:"tags"`
	Priority    int        `gorm:"default:0" json:"priority"`
	StartAt     *time.Time `json:"startAt"`
	Location    string     `gorm:"size:255" json:"location"`
	URL         string     `gorm:"size:512" json:"url"`
	Percent     int        `gorm:"default:0" json:"percent"`
	CompletedAt *time.Time `json:"completedAt"`
	ParentID    string     `gorm:"size:36;index" json:"parentId"`
	Order       int        `gorm:"default:0" json:"order"`
	Reminders   string     `gorm:"type:text" json:"-"`
	Attendees   string     `gorm:"type:text" json:"-"` // invited members (JSON), exported as ATTENDEE in VTODO
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// TodoLog records every mutation to a todo (create/update/toggle/delete) for
// the team's operation history. Details is a JSON string of changed fields.
type TodoLog struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TeamID    string    `gorm:"size:36;index" json:"teamId"`
	TodoID    string    `gorm:"size:36;index" json:"todoId"`
	UserID    string    `gorm:"size:36" json:"userId"`
	Action    string    `gorm:"size:16" json:"action"` // create|update|toggle|delete
	Details   string    `gorm:"type:text" json:"details"`
	CreatedAt time.Time `json:"createdAt"`
}

type File struct {
	ID        string     `gorm:"primaryKey;size:36" json:"id"`
	TeamID    string     `gorm:"size:36;index" json:"teamId"`
	Scope     string     `gorm:"size:16;not null;default:public" json:"scope"`
	OwnerID   string     `gorm:"size:36;index" json:"ownerId"`
	Name      string     `gorm:"size:255;not null" json:"name"`
	MimeType  string     `gorm:"size:128" json:"mimeType"`
	Size      int64      `json:"size"`
	FolderID  string     `gorm:"size:36;default:'';index" json:"folderId"`
	DeadProps string     `gorm:"type:text" json:"-"` // WebDAV dead properties (JSON)
	// AttachmentOf links a file to a calendar/todo item when set ("" = a plain
	// file). Attachment files stay visible over WebDAV but are hidden from the
	// Files page root listing; see models.Attachment.
	AttachmentOf string     `gorm:"size:36;default:'';index" json:"attachmentOf"`
	DeletedAt    *time.Time `json:"deletedAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// Attachment links an uploaded File to an event / todo / note item (Kind is
// "event" | "todo" | "note" and ItemID is the row id). The content itself
// lives in the File row so attachments are URL-accessible and show up on the
// WebDAV mount of the team/personal directory.
type Attachment struct {
	ID         string    `gorm:"primaryKey;size:36" json:"id"`
	TeamID     string    `gorm:"size:36;index:idx_att_item" json:"teamId"`
	Kind       string    `gorm:"size:16;index:idx_att_item" json:"kind"` // event | todo | note
	ItemID     string    `gorm:"size:36;index:idx_att_item" json:"itemId"`
	FileID     string    `gorm:"size:36;index" json:"fileId"`
	UserID     string    `gorm:"size:36;index" json:"userId"`
	Scope      string    `gorm:"size:16" json:"scope"` // personal | public
	CreatedAt  time.Time `json:"createdAt"`
}

// AttachmentView adds the underlying file fields and content URL.
type AttachmentView struct {
	Attachment
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}

type FileFolder struct {
	ID        string     `gorm:"primaryKey;size:36" json:"id"`
	TeamID    string     `gorm:"size:36;index" json:"teamId"`
	Scope     string     `gorm:"size:16;not null;default:public" json:"scope"`
	OwnerID   string     `gorm:"size:36;index" json:"ownerId"`
	ParentID  string     `gorm:"size:36;default:'';index" json:"parentId"`
	Name      string     `gorm:"size:255;not null" json:"name"`
	DeadProps string     `gorm:"type:text" json:"-"` // WebDAV dead properties (JSON)
	DeletedAt *time.Time `json:"deletedAt"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}
