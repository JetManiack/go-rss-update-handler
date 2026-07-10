package storage

import (
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDB initializes GORM database, configures the pool, runs migrations, and returns a Store.
func InitDB(cfg Config) (Store, *gorm.DB, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}

	var dialector gorm.Dialector
	switch cfg.Driver {
	case "postgres":
		dialector = postgres.Open(cfg.DSN)
	case "sqlite":
		dialector = sqlite.Open(cfg.DSN)
	default:
		return nil, nil, fmt.Errorf("storage: unsupported driver %q", cfg.Driver)
	}

	gormCfg := &gorm.Config{}
	if !cfg.LogQueries {
		gormCfg.Logger = logger.Default.LogMode(logger.Silent)
	} else {
		gormCfg.Logger = logger.Default.LogMode(logger.Info)
	}

	db, err := gorm.Open(dialector, gormCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: failed to open connection: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("storage: failed to get generic database interface: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	} else {
		sqlDB.SetMaxOpenConns(10)
	}
	sqlDB.SetMaxIdleConns(max(5, cfg.MaxOpenConns/2))
	sqlDB.SetConnMaxLifetime(time.Hour)

	if cfg.Driver == "sqlite" {
		if err := db.Exec("PRAGMA journal_mode=WAL;").Error; err != nil {
			return nil, nil, fmt.Errorf("storage: failed to enable WAL mode: %w", err)
		}
		if err := db.Exec("PRAGMA busy_timeout=5000;").Error; err != nil {
			return nil, nil, fmt.Errorf("storage: failed to set busy timeout: %w", err)
		}
	}

	err = db.AutoMigrate(
		&Feed{},
		&Update{},
		&RawContent{},
		&Channel{},
		&Dispatch{},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: database auto-migration failed: %w", err)
	}

	return NewStore(db), db, nil
}
