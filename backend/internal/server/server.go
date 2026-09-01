package server

import (
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"

	"homihub/backend/internal/config"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/modules"
	moduleauth "homihub/backend/internal/modules/auth"
	modulecalendar "homihub/backend/internal/modules/calendar"
	modulefamily "homihub/backend/internal/modules/family"
	modulefiles "homihub/backend/internal/modules/files"
	moduletodos "homihub/backend/internal/modules/todos"
	"gorm.io/gorm"
)

// NewStaticFS wraps an fs.FS into an http.FileSystem for use with gin.
func NewStaticFS(fsys fs.FS, root string) http.FileSystem {
	return http.FS(fsys)
}

func New(cfg *config.Config, database *gorm.DB, staticFS http.FileSystem) *gin.Engine {
	app := &modules.App{DB: database, Config: cfg}

	r := gin.New()
	r.Use(gin.Recovery(), middleware.CORS(cfg.CORSOrigin))

	api := r.Group("/api/v1")
	calMod := &modulecalendar.Handler{}
	filesMod := &modulefiles.Handler{}
	if err := modules.Mount(app, map[string]*gin.RouterGroup{
		"auth":     api,
		"family":   api,
		"calendar": api,
		"todos":    api,
		"files":    api,
	}, &moduleauth.Handler{}, &modulefamily.Handler{}, calMod,
		&moduletodos.Handler{}, filesMod); err != nil {
		panic(err)
	}

	calMod.RegisterDAV(r.Group(""), filesMod.DAVHandler())

	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })

	if staticFS != nil {
		servesSPA(r, staticFS)
	}
	return r
}

func servesSPA(r *gin.Engine, staticFS http.FileSystem) {
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if p == "/" {
			p = "/index.html"
		}
		f, err := staticFS.Open(p)
		if err == nil {
			defer f.Close()
			if fi, err := f.Stat(); err == nil && !fi.IsDir() {
				http.ServeContent(c.Writer, c.Request, p, fi.ModTime(), f)
				return
			}
		}
		f, err = staticFS.Open("/index.html")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		defer f.Close()
		fi, _ := f.Stat()
		c.Header("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(c.Writer, c.Request, "index.html", fi.ModTime(), f)
	})
}
