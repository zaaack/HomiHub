package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"homihub/backend/internal/config"
	"homihub/backend/internal/db"
	"homihub/backend/internal/server"
)

func main() {
	cfg := config.Load()
	database := db.Open(cfg)
	var staticFS http.FileSystem
	if os.Getenv("HOMIHUB_NO_STATIC") == "" {
		staticFS = server.NewStaticFS(StaticFS, "static")
	}
	r := server.New(cfg, database, staticFS)
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}

	go func() {
		log.Printf("HomiHub listening on :%s (data: %s)", cfg.Port, cfg.DataDir)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Optional HTTPS listener (LAN CalDAV clients such as tasks.org require
	// HTTPS; this lets them connect directly instead of via an external
	// reverse proxy that may drop WebDAV write methods).
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		tlsSrv := &http.Server{Addr: ":" + cfg.TLSPort, Handler: r}
		go func() {
			log.Printf("HomiHub TLS listening on :%s", cfg.TLSPort)
			if err := tlsSrv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey); err != nil && err != http.ErrServerClosed {
				log.Fatalf("tls server error: %v", err)
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	if sqlDB, err := database.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
