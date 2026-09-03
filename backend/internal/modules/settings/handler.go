package modulesettings

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	"homihub/backend/internal/modules"
)

const SettingKeyCORSOrigins = "cors_origins"
const SettingKeyTrashRetentionDays = "trash_retention_days"

// DefaultTrashRetentionDays keeps deleted files in the trash this long (days).
// A value of 0 disables automatic purging (keep forever).
const DefaultTrashRetentionDays = 90

type Handler struct {
	app  *modules.App
	cors *middleware.CORSProvider
}

func (h *Handler) ID() string { return "settings" }

func (h *Handler) Init(app *modules.App) error {
	h.app = app
	return nil
}

// SetCORSProvider wires the runtime CORS allow-list so the settings API can
// update it without a restart.
func (h *Handler) SetCORSProvider(p *middleware.CORSProvider) {
	h.cors = p
}

func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	auth := middleware.Auth(h.app.DB, h.app.Config.JWTSecret)
	g.GET("/settings/cors", auth, middleware.RequireParent(), h.getCORS)
	g.PUT("/settings/cors", auth, middleware.RequireParent(), h.putCORS)
	g.GET("/settings/trash", auth, middleware.RequireParent(), h.getTrash)
	g.PUT("/settings/trash", auth, middleware.RequireParent(), h.putTrash)
}

func (h *Handler) getCORS(c *gin.Context) {
	origins := h.cors.Origins()
	if origins == "" {
		var s models.Setting
		if err := h.app.DB.Where("key = ?", SettingKeyCORSOrigins).First(&s).Error; err == nil {
			origins = s.Value
		}
	}
	httpx.OK(c, gin.H{"origins": origins})
}

func (h *Handler) putCORS(c *gin.Context) {
	var in struct {
		Origins string `json:"origins"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if err := h.app.DB.Save(&models.Setting{Key: SettingKeyCORSOrigins, Value: in.Origins}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	h.cors.SetOrigins(in.Origins)
	httpx.OK(c, gin.H{"origins": h.cors.Origins()})
}

// TrashRetentionDays returns the configured trash retention in days. 0 means
// deleted files are kept forever (no automatic purge).
func TrashRetentionDays(db *gorm.DB) int {
	var s models.Setting
	if err := db.Where("key = ?", SettingKeyTrashRetentionDays).First(&s).Error; err != nil {
		return DefaultTrashRetentionDays
	}
	days, err := strconv.Atoi(s.Value)
	if err != nil || days < 0 {
		return DefaultTrashRetentionDays
	}
	return days
}

func (h *Handler) getTrash(c *gin.Context) {
	httpx.OK(c, gin.H{"days": TrashRetentionDays(h.app.DB)})
}

func (h *Handler) putTrash(c *gin.Context) {
	var in struct {
		Days int `json:"days"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if in.Days < 0 {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	if err := h.app.DB.Save(&models.Setting{Key: SettingKeyTrashRetentionDays, Value: strconv.Itoa(in.Days)}).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	httpx.OK(c, gin.H{"days": in.Days})
}
