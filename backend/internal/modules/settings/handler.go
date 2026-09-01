package modulesettings

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	"homihub/backend/internal/modules"
)

const SettingKeyCORSOrigins = "cors_origins"

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
