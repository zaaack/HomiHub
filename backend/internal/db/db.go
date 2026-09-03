package db

import (
	"log"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"homihub/backend/internal/config"
	"homihub/backend/internal/models"
)

func Open(cfg *config.Config) *gorm.DB {
	if cfg.DBDSN != "" && cfg.DBDSN != ":memory:" {
		if dir := filepath.Dir(cfg.DBDSN); dir != "." {
			_ = os.MkdirAll(dir, 0o755)
		}
	}
	conn, err := gorm.Open(sqlite.Open(cfg.DBDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	if sqlDB, err := conn.DB(); err == nil {
		sqlDB.Exec("PRAGMA journal_mode=WAL")
		sqlDB.Exec("PRAGMA busy_timeout=5000")
	}
	if err := Migrate(conn); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	return conn
}

func Migrate(conn *gorm.DB) error {
	if err := conn.AutoMigrate(
		&models.Team{},
		&models.User{},
		&models.TeamMember{},
		&models.Token{},
		&models.Invite{},
		&models.Calendar{},
		&models.CalendarShare{},
		&models.CalendarEvent{},
		&models.Todo{},
		&models.TodoLog{},
		&models.CalendarSyncLog{},
		&models.ScheduleMessage{},
		&models.File{},
		&models.FileFolder{},
		&models.Setting{},
	); err != nil {
		return err
	}
	// Backfill: pre-existing shared todos belong to the team calendar.
	return conn.Model(&models.Todo{}).
		Where("shared = ? AND (calendar = ? OR calendar = '' OR calendar IS NULL)", true, "self").
		Update("calendar", "team").Error
}
