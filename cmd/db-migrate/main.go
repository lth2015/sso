// db-migrate runs embedded schema migrations with dedicated database credentials.
package main

import (
	"context"
	"github.com/lth2015/sso/internal/dbmigration"
	"github.com/lth2015/sso/internal/localruntime"
	"github.com/lth2015/sso/migrations"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log.SetFlags(0)
	group := os.Getenv("MIGRATION_GROUP")
	expected := map[string]string{"business": "sso_business", "bff": "sso_console"}[group]
	if expected == "" || os.Getenv("DB_NAME") != expected || os.Getenv("SSO_MODE") != "kubernetes" {
		log.Fatal("invalid migration target")
	}
	files, err := dbmigration.Load(migrations.SQL, group)
	if err != nil {
		log.Fatal("migration bundle invalid")
	}
	db, err := localruntime.MigrationDatabase()
	if err != nil {
		log.Fatal("migration database unavailable; check credentials, TLS and network")
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err = dbmigration.Apply(ctx, db, expected, files, func(message string) { log.Print(message) }); err != nil {
		log.Fatal(err)
	}
	log.Print("Migration verified: " + group)
}
