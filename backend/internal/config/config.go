package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	Port       string
	DBDriver   string
	DBDSN      string
	JWTSecret  string
	CORSOrigin string
	DataDir    string
	StorageDir string
	CalendarTZ string
	TLSCert    string
	TLSKey     string
	TLSPort    string
}

func Load() *Config {
	port := getenv("PORT", "8080")
	dbDriver := getenv("DB_DRIVER", "sqlite")
	dataDir := getenv("HOMIHUB_DATA_DIR", "./data")
	storageDir := getenv("STORAGE_LOCAL_DIR", filepath.Join(dataDir, "storage"))
	dbDSN := getenv("DB_DSN", filepath.Join(dataDir, "db", "homihub.db"))
	return &Config{
		Port:       port,
		DBDriver:   dbDriver,
		DBDSN:      dbDSN,
		JWTSecret:  getenv("JWT_SECRET", "dev-secret-change-me"),
		CORSOrigin: getenv("CORS_ORIGINS", ""),
		DataDir:    dataDir,
		StorageDir: storageDir,
		CalendarTZ: getenv("CALENDAR_TZ", "UTC"),
		TLSCert:    getenv("HOMIHUB_TLS_CERT", ""),
		TLSKey:     getenv("HOMIHUB_TLS_KEY", ""),
		TLSPort:    getenv("HOMIHUB_TLS_PORT", "8443"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
