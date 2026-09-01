package modules

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"homihub/backend/internal/config"
)

type App struct {
	DB     *gorm.DB
	Config *config.Config
}

type Module interface {
	ID() string
	Init(app *App) error
	RegisterRoutes(g *gin.RouterGroup)
}

func Mount(app *App, groups map[string]*gin.RouterGroup, mods ...Module) error {
	for _, m := range mods {
		if err := m.Init(app); err != nil {
			return err
		}
		m.RegisterRoutes(groups[m.ID()])
	}
	return nil
}
