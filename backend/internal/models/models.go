package models

import "time"

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

type Todo struct {
	ID        string     `gorm:"primaryKey;size:36" json:"id"`
	TeamID  string     `gorm:"size:36;index" json:"teamId"`
	UserID    string     `gorm:"size:36;index" json:"userId"`
	Title     string     `gorm:"size:255;not null" json:"title"`
	Note      string     `gorm:"size:2000" json:"note"`
	Completed bool       `json:"completed"`
	Shared    bool       `gorm:"default:false" json:"shared"`
	DueAt     *time.Time `json:"dueAt"`
	RRule     string     `gorm:"size:255" json:"rrule"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
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
