// This executable is an isolated M0 laboratory, not a production identity service.
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/lth2015/sso/internal/m0policy"
)

func main() {
	if os.Getenv("SSO_MODE") != "m0-laboratory" {
		log.Fatal("M0 laboratory mode must be explicitly enabled")
	}
	key := os.Getenv("HOOK_KEY")
	if len(key) < 32 {
		log.Fatal("HOOK_KEY must contain at least 32 characters")
	}
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd = "m0_hook", os.Getenv("HOOK_DB_PASSWORD")
	cfg.Net, cfg.Addr, cfg.DBName = "tcp", "mysql:3306", "sso_m0"
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = time.Second, time.Second, time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		log.Fatal("database configuration failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	srv := &http.Server{Addr: ":8081", Handler: m0policy.Handler(db, key), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
	log.Print("M0 policy hook listening; no production use")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("HTTP server stopped unexpectedly")
	}
}
