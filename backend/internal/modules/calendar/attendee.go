package modulecalendar

import (
	"errors"
	"log"
	"time"

	"github.com/emersion/go-ical"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
)

// --- iCal ATTENDEE <-> model ---

// writeAttendeeProps emits one ATTENDEE prop per invitee (RFC 5545 §3.2.1).
func writeAttendeeProps(comp *ical.Component, as []models.Attendee) {
	for _, a := range as {
		if a.Email == "" {
			continue
		}
		p := ical.NewProp(ical.PropAttendee)
		if a.Name != "" {
			p.Params.Set("CN", a.Name)
		}
		status := a.Status
		if status == "" {
			status = models.AttendeeStatusNeedsAction
		}
		p.Params.Set("PARTSTAT", status)
		p.Params.Set("RSVP", "TRUE")
		p.Value = "mailto:" + a.Email
		comp.Props.Add(p)
	}
}

// readAttendeeProps decodes ATTENDEE props (mailto values + CN/PARTSTAT).
func readAttendeeProps(comp *ical.Component) []models.Attendee {
	out := []models.Attendee{}
	for _, p := range comp.Props.Values(ical.PropAttendee) {
		email := normalizeAddress(p.Value)
		if email == "" {
			continue
		}
		status := p.Params.Get("PARTSTAT")
		if status == "" {
			status = models.AttendeeStatusNeedsAction
		}
		a := models.Attendee{Email: email, Status: status}
		if cn := p.Params.Get("CN"); cn != "" {
			a.Name = cn
		}
		out = append(out, a)
	}
	return out
}

// --- invitee resolution ---

// ResolveInvitees validates member ids against the team and returns the
// attendees (with status NEEDS-ACTION) plus the matching user rows. Invalid or
// foreign ids are silently dropped. Callers exclude the organizer themselves.
// Each query runs on its own fresh scoped session (GORM reuses the underlying
// statement, so chaining two queries on one handle would merge their WHEREs).
func ResolveInvitees(base *gorm.DB, teamID string, ids []string) ([]models.Attendee, []models.User) {
	sc := func() *gorm.DB { return middleware.ScopedDB(base, teamID) }
	seen := map[string]bool{}
	unique := []string{}
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return nil, nil
	}
	var rows []models.TeamMember
	if err := sc().Where("user_id IN ?", unique).Find(&rows).Error; err != nil {
		return nil, nil
	}
	memberIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		memberIDs = append(memberIDs, r.UserID)
	}
	if len(memberIDs) == 0 {
		return nil, nil
	}
	var users []models.User
	if err := sc().Where("id IN ?", memberIDs).Find(&users).Error; err != nil {
		return nil, nil
	}
	byID := map[string]*models.User{}
	order := []string{}
	for i := range users {
		byID[users[i].ID] = &users[i]
		order = append(order, users[i].ID)
	}
	attendees := make([]models.Attendee, 0, len(users))
	for _, id := range order {
		u := byID[id]
		attendees = append(attendees, models.Attendee{
			ID:     u.ID,
			Name:   u.Name,
			Email:  u.Email,
			Status: models.AttendeeStatusNeedsAction,
		})
	}
	return attendees, users
}

// needsInviteCopy reports whether the invitee cannot already see the item (in
// which case a private copy in their personal calendar is created). Whole-team
// items and shared lists need no copies.
func needsInviteCopy(db *gorm.DB, teamID, calName, inviteeID string) bool {
	switch calName {
	case calSelf:
		return true
	case calTeam:
		return false
	}
	// Custom calendars: legacy rows are whole-team; member-scoped lists only
	// copy when the invitee has no access to the list itself.
	return TodoListRole(db, teamID, inviteeID, calName) == ""
}

// SyncEventInvites reconciles invitee personal copies with the organizer's
// attendee set: upserts copies for invitees that cannot already see the event,
// removes copies of former invitees or of invitees that now see it directly.
// Shared by VEVENT (events) and VJOURNAL (notes) rows.
func SyncEventInvites(base *gorm.DB, teamID string, ev *models.CalendarEvent, users []models.User, prev []models.Attendee, selfID string) {
	sc := func() *gorm.DB { return middleware.ScopedDB(base, teamID) }
	curIDs := map[string]bool{}
	for _, u := range users {
		if u.ID == "" || u.ID == selfID {
			continue
		}
		curIDs[u.ID] = true
		if !needsInviteCopy(sc(), teamID, ev.Calendar, u.ID) {
			continue // already visible to the invitee: no duplicate copy
		}
		if err := UpsertEventInviteeCopy(base, teamID, ev, &u); err != nil {
			log.Printf("event invite copy upsert: %v", err)
		}
	}
	removals := map[string]bool{}
	for _, a := range prev {
		if a.ID == "" {
			continue
		}
		if !curIDs[a.ID] || !needsInviteCopy(sc(), teamID, ev.Calendar, a.ID) {
			removals[a.ID] = true
		}
	}
	if len(removals) == 0 {
		return
	}
	ids := make([]string, 0, len(removals))
	for id := range removals {
		ids = append(ids, id)
	}
	_ = DeleteEventInviteeCopies(sc(), ev.UID, ids)
}

// --- personal-calendar copies (events) ---

// UpsertEventInviteeCopy mirrors an organizer event into the invitee's personal
// self calendar (same UID, private). Keeps the row's own visibility/user.
func UpsertEventInviteeCopy(base *gorm.DB, teamID string, org *models.CalendarEvent, u *models.User) error {
	sc := func() *gorm.DB { return middleware.ScopedDB(base, teamID) }
	var row models.CalendarEvent
	err := sc().Where("user_id = ? AND calendar = ? AND uid = ? AND recurrence_id IS NULL", u.ID, calSelf, org.UID).First(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	created := errors.Is(err, gorm.ErrRecordNotFound)
	if created {
		row = models.CalendarEvent{
			ID:            uuid.Must(uuid.NewV7()).String(),
			TeamID:        org.TeamID,
			UserID:        u.ID,
			UID:           org.UID,
			Calendar:      calSelf,
			ComponentType: org.ComponentType,
			Visibility:    models.VisibilityPrivate,
		}
		if row.ComponentType == "" {
			row.ComponentType = ical.CompEvent
		}
	}
	now := time.Now().UTC()
	row.Title = org.Title
	row.Category = org.Category
	row.Location = org.Location
	row.Description = org.Description
	row.Tags = org.Tags
	row.Class = org.Class
	row.Duration = org.Duration
	row.StartsAt = org.StartsAt
	row.EndsAt = org.EndsAt
	row.AllDay = org.AllDay
	row.RRule = org.RRule
	row.ExDate = org.ExDate
	row.RelatedTo = org.RelatedTo
	row.Reminders = org.Reminders
	row.Attendees = org.Attendees
	row.UpdatedAt = now
	if created {
		return sc().Create(&row).Error
	}
	return sc().Save(&row).Error
}

// DeleteEventInviteeCopies permanently removes invitee personal copies (master
// + any exception rows share the same UID) when they are no longer invited.
func DeleteEventInviteeCopies(db *gorm.DB, uid string, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	return db.Unscoped().Where("uid = ? AND calendar = ? AND user_id IN ?", uid, calSelf, userIDs).
		Delete(&models.CalendarEvent{}).Error
}

// SoftDeleteEventInviteeCopies soft-deletes invitee personal copies (same UID)
// when the organizer's event/note is deleted, so they travel to the trash too.
func SoftDeleteEventInviteeCopies(db *gorm.DB, uid string, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	return db.Where("uid = ? AND calendar = ? AND user_id IN ?", uid, calSelf, userIDs).
		Delete(&models.CalendarEvent{}).Error
}

// --- personal-calendar copies (todos) ---

// UpsertTodoInviteeCopy mirrors an organizer todo into the invitee's personal
// self list (same UID). The invitee's local completion/progress/order is kept.
func UpsertTodoInviteeCopy(base *gorm.DB, teamID string, org *models.Todo, u *models.User) error {
	sc := func() *gorm.DB { return middleware.ScopedDB(base, teamID) }
	var row models.Todo
	err := sc().Where("user_id = ? AND calendar = ? AND uid = ?", u.ID, calSelf, org.UID).First(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	created := errors.Is(err, gorm.ErrRecordNotFound)
	if created {
		row = models.Todo{
			ID:       uuid.Must(uuid.NewV7()).String(),
			TeamID:   org.TeamID,
			UserID:   u.ID,
			UID:      org.UID,
			Calendar: calSelf,
			Shared:   false,
		}
	}
	now := time.Now().UTC()
	row.Title = org.Title
	row.Note = org.Note
	row.Location = org.Location
	row.URL = org.URL
	row.StartAt = org.StartAt
	row.DueAt = org.DueAt
	row.RRule = org.RRule
	row.ExDate = org.ExDate
	row.Tags = org.Tags
	row.Priority = org.Priority
	row.ParentID = org.ParentID
	row.Reminders = org.Reminders
	row.Attendees = org.Attendees
	row.UpdatedAt = now
	// Preserve the invitee's own completion state — never overwrite it.
	if created {
		return sc().Create(&row).Error
	}
	return sc().Save(&row).Error
}

// DeleteTodoInviteeCopies permanently removes the invitee personal copies.
func DeleteTodoInviteeCopies(db *gorm.DB, uid string, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	return db.Unscoped().Where("uid = ? AND calendar = ? AND user_id IN ?", uid, calSelf, userIDs).
		Delete(&models.Todo{}).Error
}

// SoftDeleteTodoInviteeCopies soft-deletes the invitee personal copies so they
// travel to the trash alongside the organizer's deleted todo.
func SoftDeleteTodoInviteeCopies(db *gorm.DB, uid string, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	return db.Where("uid = ? AND calendar = ? AND user_id IN ?", uid, calSelf, userIDs).
		Delete(&models.Todo{}).Error
}

// RestoreEventInviteeCopies clears deleted_at on the soft-deleted invitee
// personal copies sharing the same UID (used when restoring a trashed event).
func RestoreEventInviteeCopies(db *gorm.DB, uid string) error {
	return db.Unscoped().Model(&models.CalendarEvent{}).
		Where("uid = ? AND calendar = ?", uid, calSelf).
		Updates(map[string]any{"deleted_at": nil}).Error
}

// RestoreTodoInviteeCopies clears deleted_at on the soft-deleted invitee
// personal copies sharing the same UID (used when restoring a trashed todo).
func RestoreTodoInviteeCopies(db *gorm.DB, uid string) error {
	return db.Unscoped().Model(&models.Todo{}).
		Where("uid = ? AND calendar = ?", uid, calSelf).
		Updates(map[string]any{"deleted_at": nil}).Error
}

// PurgeEventInviteeCopies permanently removes the soft-deleted invitee
// personal copies sharing the UID when a trashed event is purged. Live rows
// (e.g. recurring exceptions) are left untouched.
func PurgeEventInviteeCopies(db *gorm.DB, uid string) error {
	return db.Unscoped().Where("uid = ? AND calendar = ? AND deleted_at IS NOT NULL", uid, calSelf).
		Delete(&models.CalendarEvent{}).Error
}

// PurgeTodoInviteeCopies permanently removes the soft-deleted invitee personal
// copies sharing the UID when a trashed todo is purged.
func PurgeTodoInviteeCopies(db *gorm.DB, uid string) error {
	return db.Unscoped().Where("uid = ? AND calendar = ? AND deleted_at IS NOT NULL", uid, calSelf).
		Delete(&models.Todo{}).Error
}

// SyncTodoInvites reconciles invitee personal todo copies with the organizer's
// attendee set (see SyncEventInvites for the semantics). Invitees keep their
// local completion state; only content is refreshed.
func SyncTodoInvites(base *gorm.DB, teamID string, org *models.Todo, users []models.User, prev []models.Attendee, selfID string) {
	sc := func() *gorm.DB { return middleware.ScopedDB(base, teamID) }
	curIDs := map[string]bool{}
	for _, u := range users {
		if u.ID == "" || u.ID == selfID {
			continue
		}
		curIDs[u.ID] = true
		if !needsInviteCopy(sc(), teamID, org.Calendar, u.ID) {
			continue // whole-team / shared-list items are already visible
		}
		_ = UpsertTodoInviteeCopy(base, teamID, org, &u)
	}
	removals := map[string]bool{}
	for _, a := range prev {
		if a.ID == "" {
			continue
		}
		if !curIDs[a.ID] || !needsInviteCopy(sc(), teamID, org.Calendar, a.ID) {
			removals[a.ID] = true
		}
	}
	if len(removals) == 0 {
		return
	}
	ids := make([]string, 0, len(removals))
	for id := range removals {
		ids = append(ids, id)
	}
	_ = DeleteTodoInviteeCopies(sc(), org.UID, ids)
}
