// Package localruntime configures local processes and explicit Kubernetes database connections.
package localruntime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"errors"
	driver "github.com/go-sql-driver/mysql"
	"net"
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
	case "console":
		user = "console_bff"
		dbname = "local_console_state"
		password = os.Getenv("DEMO_DB_PASSWORD")
	case "":
	default:
		return nil, errors.New("unknown local database role")
	}
	if os.Getenv("SSO_MODE") == "kubernetes" {
		return kubernetesDatabase()
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

// kubernetesDatabase uses explicit credentials and verified TLS, never local defaults.
func kubernetesDatabase() (*sql.DB, error) {
	cfg, err := kubernetesDatabaseConfig()
	if err != nil {
		return nil, err
	}
	connector, err := driver.NewConnector(cfg)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("database unavailable")
	}
	return db, nil
}
func kubernetesDatabaseConfig() (*driver.Config, error) {
	host, port := os.Getenv("DB_HOST"), os.Getenv("DB_PORT")
	if port == "" {
		port = "3306"
	}
	user, name, password := os.Getenv("DB_USER"), os.Getenv("DB_NAME"), os.Getenv("DB_PASSWORD")
	if host == "" || user == "" || name == "" || password == "" {
		return nil, errors.New("missing database configuration")
	}
	ca, err := os.ReadFile(os.Getenv("DB_TLS_CA_FILE"))
	if err != nil {
		return nil, errors.New("database CA unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("invalid database CA")
	}
	cfg := driver.NewConfig()
	cfg.User, cfg.Passwd, cfg.DBName = user, password, name
	cfg.Net, cfg.Addr, cfg.ParseTime = "tcp", net.JoinHostPort(host, port), true
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 2*time.Second, 5*time.Second, 5*time.Second
	cfg.TLS = &tls.Config{RootCAs: roots, ServerName: host, MinVersion: tls.VersionTLS12}
	return cfg, nil
}
