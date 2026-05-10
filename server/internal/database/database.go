// Package database provides SQLite database initialization and migration.
//
// This package handles:
//   - Database connection setup with WAL mode for concurrent access
//   - Auto-migration of all model tables
//   - Default data seeding (search config, DB version)
//
// SQLite configuration:
//   - WAL journal mode for better concurrent read performance
//   - 5-second busy timeout for write contention
//   - Immediate transaction locking to prevent deadlocks
//   - Single connection pool (SQLite limitation)
package database

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"private-buddy-server/internal/config"
	applogger "private-buddy-server/internal/logger"
	"private-buddy-server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// DB is the global database connection instance.
//
// NOTE: *gorm.DB exposes full database capabilities including dangerous operations
// (Raw, Exec, Migrator, DB, Callback, etc.) that violate the principle of least
// privilege. For an internal application this risk is acceptable, but if stricter
// access control is needed in the future, consider encapsulating *gorm.DB within
// this package and exposing only business-semantic functions (e.g. FindByID,
// CreateEntity, UpdateEntity) so that *gorm.DB never leaks outside this package.
var DB *gorm.DB

// Init initializes the SQLite database connection.
// Creates the database directory if it doesn't exist.
// Configures WAL mode, busy timeout, and immediate transaction locking.
func Init() {
	settings := config.Get()

	dbDir := filepath.Join(settings.DataRoot, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		panic(fmt.Sprintf("Failed to create database directory: %v", err))
	}

	dbPath := settings.DatabaseURL()
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate"

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
		NowFunc: func() time.Time {
			return time.Now().Local()
		},
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to connect to database: %v", err))
	}

	sqlDB, err := db.DB()
	if err != nil {
		panic(fmt.Sprintf("Failed to get underlying sql.DB: %v", err))
	}
	sqlDB.SetMaxOpenConns(1)

	DB = db
	applogger.L.Info("Database initialized", "path", dbPath)
}

// AutoMigrate creates all database tables and adds missing columns.
// For new tables: uses GORM AutoMigrate to create the full schema.
// For existing tables: only adds missing columns via ALTER TABLE ADD COLUMN,
// avoiding the table-recreation path that SQLite uses when schema differs
// (which can fail on NOT NULL constraints during data copy).
func AutoMigrate() {
	models := []interface{}{
		&model.LLMConfig{},
		&model.EmbeddingConfig{},
		&model.Agent{},
		&model.Session{},
		&model.Message{},
		&model.Interaction{},
		&model.HistoricalSummary{},
		&model.SearchConfig{},
		&model.DBVersion{},
		&model.KnowledgeBase{},
		&model.Document{},
		&model.DocumentChunk{},
	}

	for _, m := range models {
		if DB.Migrator().HasTable(m) {
			addMissingColumns(m)
		} else {
			if err := DB.AutoMigrate(m); err != nil {
				panic(fmt.Sprintf("Failed to auto-migrate %T: %v", m, err))
			}
			applogger.L.Info("Created table", "model", fmt.Sprintf("%T", m))
		}
	}

	ensureSearchConfig()
	ensureDBVersion()
	applogger.L.Info("Database migration completed")
}

// addMissingColumns inspects the model struct and adds any columns that
// don't exist in the database table. Uses ALTER TABLE ADD COLUMN which
// SQLite supports without table recreation.
func addMissingColumns(m interface{}) {
	stmt := &gorm.Statement{DB: DB}
	if err := stmt.Parse(m); err != nil {
		return
	}

	for _, field := range stmt.Schema.Fields {
		colName := field.DBName
		if !DB.Migrator().HasColumn(m, colName) {
			applogger.L.Info("Adding missing column", "table", stmt.Table, "column", colName)
			if err := DB.Migrator().AddColumn(m, colName); err != nil {
				panic(fmt.Sprintf("Failed to add column %s.%s: %v", stmt.Table, colName, err))
			}
		}
	}
}

// ensureSearchConfig creates the default search config record if it doesn't exist.
func ensureSearchConfig() {
	var count int64
	DB.Model(&model.SearchConfig{}).Where("id = ?", 1).Count(&count)
	if count == 0 {
		DB.Create(&model.SearchConfig{
			Provider:    "tavily",
			APIKey:      "",
			Description: "",
			IsActive:    false,
		})
	}
}

// ensureDBVersion creates the initial DB version record if the table is empty.
func ensureDBVersion() {
	var count int64
	DB.Model(&model.DBVersion{}).Count(&count)
	if count == 0 {
		DB.Create(&model.DBVersion{
			Version:     "0.0.8",
			Description: "Initial SQLite schema after MySQL migration",
		})
	}
}
