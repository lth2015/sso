// Package localruntime configures only the isolated local development processes.
package localruntime

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	driver "github.com/go-sql-driver/mysql"
	"os"
	"time"
)

func Database() (*sql.DB, error) {
	password := os.Getenv("M1_DB_PASSWORD")
	if len(password) != 64 {
		return nil, errors.New("missing local business credential")
	}
	cfg := driver.NewConfig()
	cfg.User = "business_app"
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = "mysql:3306"
	cfg.DBName = "sso_business"
	cfg.ParseTime = true
	cfg.Timeout = 2 * time.Second
	cfg.ReadTimeout = 5 * time.Second
	cfg.WriteTimeout = 5 * time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("database unavailable")
	}
	return db, nil
}
func MailKey() ([]byte, error) {
	key, err := hex.DecodeString(os.Getenv("IDENTITY_MAIL_KEY"))
	if err != nil || len(key) != 32 {
		return nil, errors.New("missing local mail encryption key")
	}
	return key, nil
}
