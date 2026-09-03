package attachments

import (
	"time"

	"gorm.io/gorm"

	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
)

// ViewsForItem lists the live attachments of an item (its rows + the linked
// File metadata and content URL). Each query runs on its own fresh scoped
// session (GORM reuses the underlying statement, so chaining queries on one
// handle would merge their WHEREs).
func ViewsForItem(base *gorm.DB, teamID, kind, itemID string) []models.AttachmentView {
	sc := func() *gorm.DB { return middleware.ScopedDB(base, teamID) }
	var atts []models.Attachment
	if err := sc().Where("kind = ? AND item_id = ?", kind, itemID).Find(&atts).Error; err != nil {
		return nil
	}
	if len(atts) == 0 {
		return nil
	}
	ids := make([]string, 0, len(atts))
	byID := make(map[string]models.Attachment, len(atts))
	for i := range atts {
		ids = append(ids, atts[i].FileID)
		byID[atts[i].FileID] = atts[i]
	}
	var files []models.File
	if err := sc().Where("id IN ?", ids).Find(&files).Error; err != nil {
		return nil
	}
	out := make([]models.AttachmentView, 0, len(files))
	for i := range files {
		a, ok := byID[files[i].ID]
		if !ok {
			continue
		}
		out = append(out, models.AttachmentView{
			Attachment: a,
			Name:       files[i].Name,
			MimeType:   files[i].MimeType,
			Size:       files[i].Size,
			URL:        "/api/v1/files/" + files[i].ID + "/content",
		})
	}
	return out
}

// DeleteOne soft-deletes an attachment's File and removes the link row.
func DeleteOne(base *gorm.DB, teamID string, a models.Attachment) error {
	sc := func() *gorm.DB { return middleware.ScopedDB(base, teamID) }
	now := time.Now()
	if err := sc().Model(&models.File{}).Where("id = ?", a.FileID).Update("deleted_at", &now).Error; err != nil {
		return err
	}
	return sc().Where("id = ?", a.ID).Delete(&models.Attachment{}).Error
}

// DeleteForItem removes every attachment of an item together with its Files.
// Used when the owning event / todo / note is deleted.
func DeleteForItem(base *gorm.DB, teamID, itemID string) error {
	sc := func() *gorm.DB { return middleware.ScopedDB(base, teamID) }
	var atts []models.Attachment
	if err := sc().Where("item_id = ?", itemID).Find(&atts).Error; err != nil {
		return err
	}
	if len(atts) == 0 {
		return nil
	}
	ids := make([]string, 0, len(atts))
	for _, a := range atts {
		ids = append(ids, a.FileID)
	}
	now := time.Now()
	if err := sc().Model(&models.File{}).Where("id IN ?", ids).Update("deleted_at", &now).Error; err != nil {
		return err
	}
	return sc().Where("item_id = ?", itemID).Delete(&models.Attachment{}).Error
}
