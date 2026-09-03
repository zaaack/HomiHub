package modulefiles

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	modulesettings "homihub/backend/internal/modules/settings"
)

// StartTrashCleaner runs the daily 3am purge of expired trash files.
func (h *Handler) StartTrashCleaner() {
	c := cron.New()
	if _, err := c.AddFunc("0 3 * * *", h.purgeExpiredTrash); err == nil {
		c.Start()
		// Run once at startup so long-expired files are cleaned even if the
		// server was down overnight.
		go h.purgeExpiredTrash()
	}
}

// listTrash returns the soft-deleted files the caller may see (public scope,
// or personal files they own; parents see everything).
func (h *Handler) listTrash(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	scope := c.DefaultQuery("scope", models.ScopePublic)
	q := middleware.DB(c).Unscoped().Where("scope = ? AND deleted_at IS NOT NULL", scope)
	if scope == models.ScopePersonal && cl.Role != middleware.RoleParent {
		q = q.Where("owner_id = ?", cl.UserID)
	}
	var files []models.File
	if err := q.Order("deleted_at desc").Find(&files).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "query_failed")
		return
	}
	httpx.OK(c, files)
}

// loadTrashFile loads a soft-deleted file row (including deleted ones).
func (h *Handler) loadTrashFile(c *gin.Context, id string) (*models.File, bool) {
	var f models.File
	if err := middleware.DB(c).Unscoped().First(&f, "id = ?", id).Error; err != nil || f.DeletedAt == nil {
		httpx.NotFoundT(c, "file_not_found")
		return nil, false
	}
	return &f, true
}

// restoreTrash clears deleted_at so the file comes back to life. If the file
// was an item attachment whose link row was already removed, it is restored as
// a plain file (attachment_of cleared) so it shows up in the Files listing.
func (h *Handler) restoreTrash(c *gin.Context) {
	f, ok := h.loadTrashFile(c, c.Param("id"))
	if !ok {
		return
	}
	if !h.canEdit(c, f) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	updates := map[string]any{"deleted_at": nil}
	if f.AttachmentOf != "" {
		var cnt int64
		if err := middleware.DB(c).Model(&models.Attachment{}).Where("file_id = ?", f.ID).Count(&cnt).Error; err == nil && cnt == 0 {
			updates["attachment_of"] = ""
		}
	}
	if err := middleware.DB(c).Model(&models.File{}).Where("id = ?", f.ID).Updates(updates).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}

// purgeTrashFile permanently deletes a trash file: content blob, any
// attachment link rows, and the row itself.
func (h *Handler) purgeTrashFile(c *gin.Context) {
	f, ok := h.loadTrashFile(c, c.Param("id"))
	if !ok {
		return
	}
	if !h.canEdit(c, f) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	sc := func() *gorm.DB { return middleware.ScopedDB(h.app.DB, f.TeamID) }
	_ = h.st.Delete(c.Request.Context(), contentKey(f.TeamID, f.Scope, f.ID))
	sc().Where("file_id = ?", f.ID).Delete(&models.Attachment{})
	if err := sc().Unscoped().Where("id = ?", f.ID).Delete(&models.File{}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "delete_failed")
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}

// purgeExpiredTrash permanently removes files whose deleted_at is older than
// the configured retention. retention == 0 disables purging. Called by the
// daily cron (3am).
func (h *Handler) purgeExpiredTrash() {
	days := modulesettings.TrashRetentionDays(h.app.DB)
	if days <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	var files []models.File
	if err := h.app.DB.Unscoped().Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff).Find(&files).Error; err != nil {
		return
	}
	for i := range files {
		f := files[i]
		sc := func() *gorm.DB { return middleware.ScopedDB(h.app.DB, f.TeamID) }
		_ = h.st.Delete(context.Background(), contentKey(f.TeamID, f.Scope, f.ID))
		sc().Where("file_id = ?", f.ID).Delete(&models.Attachment{})
		_ = sc().Unscoped().Where("id = ?", f.ID).Delete(&models.File{}).Error
	}
}
