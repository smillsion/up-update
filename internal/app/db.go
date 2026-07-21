package app

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

func openDB(cfg Config) (*sql.DB, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	if err = migrateSettings(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate settings: %w", err)
	}
	if err = ensureAdmin(db, cfg); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateSettings(db *sql.DB) error {
	integrationColumns := []struct {
		name       string
		definition string
	}{
		{"bark_quiet_enabled", "INTEGER NOT NULL DEFAULT 0"},
		{"bark_quiet_start", "TEXT NOT NULL DEFAULT '12:00'"},
		{"bark_quiet_end", "TEXT NOT NULL DEFAULT '14:00'"},
	}
	for _, column := range integrationColumns {
		exists, err := tableHasColumn(db, "integrations", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.Exec("ALTER TABLE integrations ADD COLUMN " + column.name + " " + column.definition); err != nil {
				return err
			}
		}
	}
	deliveryColumns := []struct {
		name       string
		definition string
	}{
		{"deferred_until", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, column := range deliveryColumns {
		exists, err := tableHasColumn(db, "deliveries", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.Exec("ALTER TABLE deliveries ADD COLUMN " + column.name + " " + column.definition); err != nil {
				return err
			}
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM app_settings WHERE key=?`, pollScheduleKey).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		legacyInterval := 300
		_ = db.QueryRow(`SELECT CAST(value AS INTEGER) FROM app_settings WHERE key='poll_interval_seconds'`).Scan(&legacyInterval)
		encoded, err := encodePollSchedule(defaultPollSchedule(legacyInterval))
		if err != nil {
			return err
		}
		_, err = db.Exec(`INSERT INTO app_settings(key,value) VALUES(?,?)`, pollScheduleKey, encoded)
		return err
	}
	return nil
}

func tableHasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func ensureAdmin(db *sql.DB, cfg Config) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if len(cfg.AdminPassword) < 10 {
		return errors.New("UP_UPDATE_ADMIN_PASSWORD must contain at least 10 characters on first startup")
	}
	hash, err := hashPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := db.ExecContext(ctx, `INSERT INTO users(username,display_name,password_hash,role,created_at) VALUES(?,?,?,?,?)`, cfg.AdminUsername, "管理员", hash, "admin", time.Now().Unix())
	if err != nil {
		return err
	}
	userID, _ := result.LastInsertId()
	_, err = db.ExecContext(ctx, `INSERT INTO integrations(user_id,bark_server,updated_at) VALUES(?,?,?)`, userID, cfg.DefaultBarkServer, time.Now().Unix())
	return err
}
