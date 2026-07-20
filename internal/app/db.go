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
	if err = ensureAdmin(db, cfg); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
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
