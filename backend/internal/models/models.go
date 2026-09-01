package models

import (
	"encoding/json"
	"time"
)

// Visibility constants for calendar events.
const (
	VisibilityPrivate = 1 // Only owner
	VisibilityBusy    = 2 // Others see busy, no details
	VisibilityTeam  = 3 // Everyone (default)
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
	TeamID     string    `gorm:"size:36;index" json:"teamId"`
	Email        string    `gorm:"size:255;uniqueIndex;not null" json:"email"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Name         string    `gorm:"size:64;not null" json:"name"`
	Role         string    `gorm:"size:16;default:child" json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// TeamMember is the multi-space membership join table.
type TeamMember struct {
	UserID   string `gorm:"primaryKey;size:36" json:"userId"`
	TeamID string `gorm:"primaryKey;size:36" json:"teamId"`
	Role     string `gorm:"size:16;default:child" json:"role"`
}

type Token struct {
	ID         string     `gorm:"primaryKey;size:36" json:"id"`
	TeamID   string     `gorm:"size:36;index" json:"teamId"`
	Kind       string     `gorm:"size:16;not null" json:"kind"`
	SubjectID  string     `gorm:"size:36" json:"subjectId"`
	Name       string     `gorm:"size:128" json:"name"`
	TokenHash  string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
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
	TeamID  string     `gorm:"size:36;index" json:"teamId"`
	TokenHash string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	Role      string     `gorm:"size:16;default:child" json:"role"`
	ExpiresAt time.Time  `json:"expiresAt"`
	UsedAt    *time.Time `json:"usedAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

type CalendarEvent struct {
	ID          string     `gorm:"primaryKey;size:36" json:"id"`
	TeamID    string     `gorm:"size:36;index" json:"teamId"`
	UserID      string     `gorm:"size:36;index" json:"userId"`
	UID         string     `gorm:"size:64;index" json:"uid"`
	Title       string     `gorm:"size:255;not null" json:"title"`
	Category    string     `gorm:"size:16;not null;default:family" json:"category"`
	Location    string     `gorm:"size:255" json:"location"`
	Description string     `gorm:"size:2000" json:"description"`
	StartsAt    time.Time  `json:"startsAt"`
	EndsAt      time.Time  `json:"endsAt"`
	AllDay      bool       `json:"allDay"`
	RRule       string     `gorm:"size:255" json:"rrule"`
	RecurrenceID *time.Time `json:"recurrenceId"`
	Visibility  int        `gorm:"default:3" json:"visibility"`
	Attendees   string     `gorm:"type:text" json:"-"`
	Reminders   string     `gorm:"type:text" json:"-"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// Setting is a global key-value store (not tenant scoped).
type Setting struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"size:4096" json:"value"`
}

// Reminder is a relative pre-notification (e.g. 10 minutes before), stored as
// a JSON array on the calendar/todo model and serialized to VALARM in iCal.
type Reminder struct {
	Unit  string `json:"unit"` // min|hour|day
	Value int    `json:"value"`
}

// Seconds returns the absolute duration this reminder represents.
func (r Reminder) Seconds() int {
	switch r.Unit {
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

type Todo struct {
	ID          string     `gorm:"primaryKey;size:36" json:"id"`
	TeamID      string     `gorm:"size:36;index" json:"teamId"`
	UserID      string     `gorm:"size:36;index" json:"userId"`
	UID         string     `gorm:"size:64;index" json:"uid"`
	Title       string     `gorm:"size:255;not null" json:"title"`
	Note        string     `gorm:"size:2000" json:"note"`
	Completed   bool       `json:"completed"`
	Shared      bool       `gorm:"default:false" json:"shared"`
	DueAt       *time.Time `json:"dueAt"`
	RRule       string     `gorm:"size:255" json:"rrule"`
	Group       string     `gorm:"size:64;index" json:"group"`
	Tags        string     `gorm:"size:500" json:"tags"`
	Priority    int        `gorm:"default:0" json:"priority"`
	StartAt     *time.Time `json:"startAt"`
	Location    string     `gorm:"size:255" json:"location"`
	URL         string     `gorm:"size:512" json:"url"`
	Percent     int        `gorm:"default:0" json:"percent"`
	CompletedAt *time.Time `json:"completedAt"`
	ParentID    string     `gorm:"size:36;index" json:"parentId"`
	Reminders   string     `gorm:"type:text" json:"-"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type File struct {
	ID             string     `gorm:"primaryKey;size:36" json:"id"`
	TeamID       string     `gorm:"size:36;index" json:"teamId"`
	Scope          string     `gorm:"size:16;not null;default:public" json:"scope"`
	OwnerID        string     `gorm:"size:36;index" json:"ownerId"`
	Name           string     `gorm:"size:255;not null" json:"name"`
	MimeType       string     `gorm:"size:128" json:"mimeType"`
	Size           int64      `json:"size"`
	FolderID       string     `gorm:"size:36;default:'';index" json:"folderId"`
	DeletedAt      *time.Time `json:"deletedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type FileFolder struct {
	ID        string     `gorm:"primaryKey;size:36" json:"id"`
	TeamID  string     `gorm:"size:36;index" json:"teamId"`
	Scope     string     `gorm:"size:16;not null;default:public" json:"scope"`
	OwnerID   string     `gorm:"size:36;index" json:"ownerId"`
	ParentID  string     `gorm:"size:36;default:'';index" json:"parentId"`
	Name      string     `gorm:"size:255;not null" json:"name"`
	DeletedAt *time.Time `json:"deletedAt"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}
