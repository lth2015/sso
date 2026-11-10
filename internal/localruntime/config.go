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
	user, dbname, password := "business_app", "sso_business", os.Getenv("M1_DB_PASSWORD")
	switch os.Getenv("LOCAL_DB_KIND") {
	case "demo_a":
		user = "demo_a"
		dbname = "local_demo_a_state"
		password = os.Getenv("DEMO_DB_PASSWORD")
	case "demo_b":
		user = "demo_b"
		dbname = "local_demo_b_state"
		password = os.Getenv("DEMO_DB_PASSWORD")
	case "":
	default:
		return nil, errors.New("unknown local database role")
	}
	if len(password) != 64 {
		return nil, errors.New("missing local database credential")
	}
	cfg := driver.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = "mysql:3306"
	cfg.DBName = dbname
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
