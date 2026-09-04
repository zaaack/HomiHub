package moduletrash

import (
	"github.com/gin-gonic/gin"

	"homihub/backend/internal/middleware"
	"homihub/backend/internal/modules"
)

type Handler struct {
	app *modules.App
}

func (h *Handler) ID() string { return "trash" }

func (h *Handler) Init(app *modules.App) error {
	h.app = app
	return nil
}

func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	auth := middleware.Auth(h.app.DB, h.app.Config.JWTSecret)
	g.GET("/trash/items", auth, h.listTrash)
	g.POST("/trash/items/:kind/:id/restore", auth, middleware.RequireWriteRole(), h.restoreTrash)
	g.DELETE("/trash/items/:kind/:id", auth, middleware.RequireWriteRole(), h.purgeTrash)
}
