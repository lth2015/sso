package dbmigration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"testing/fstest"
	"time"

	driver "github.com/go-sql-driver/mysql"
	bundle "github.com/lth2015/sso/migrations"
)

func testDatabase(t *testing.T) (*sql.DB, string) {
	t.Helper()
	addr := os.Getenv("SSO_MIGRATION_TEST_ADDR")
	if addr == "" {
		t.Skip("requires disposable MySQL from scripts/test-migrations")
	}
	cfg := driver.NewConfig()
	cfg.User = "root"
	cfg.Passwd = "local-migration-test-only"
	cfg.Net = "tcp"
	cfg.Addr = addr
	cfg.MultiStatements = true
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("migration_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	cfg.DBName = name
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); admin.Exec("DROP DATABASE " + name); admin.Close() })
	return db, name
}
func silent(string) {}
func TestMigrationLifecycle(t *testing.T) {
	db, name := testDatabase(t)
	ctx := context.Background()
	source := fstest.MapFS{"business/001_probe.sql": {Data: []byte("CREATE TABLE probe (id INT); INSERT INTO probe VALUES(1);")}}
	files, err := Load(source, "business")
	if err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, db, name, files, silent); err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, db, name, files, silent); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM probe").Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration replayed: %d %v", count, err)
	}
	changed := append([]Migration(nil), files...)
	changed[0].Checksum = "modified"
	if err = Apply(ctx, db, name, changed, silent); err == nil {
		t.Fatal("checksum change accepted")
	}
	if err = Apply(ctx, db, name, nil, silent); err == nil {
		t.Fatal("downgrade accepted")
	}
}
func TestPartialDDLBlocksAutomaticRetry(t *testing.T) {
	db, name := testDatabase(t)
	files := []Migration{{"001_partial", "CREATE TABLE partial (id INT); INSERT INTO missing_table VALUES(1)", "expected"}}
	if err := Apply(context.Background(), db, name, files, silent); err == nil {
		t.Fatal("invalid DDL accepted")
	}
	var checksum string
	if err := db.QueryRow("SELECT checksum FROM schema_migrations WHERE version='001_partial'").Scan(&checksum); err != nil || checksum != "incomplete" {
		t.Fatalf("partial DDL not recorded: %s %v", checksum, err)
	}
	if err := Apply(context.Background(), db, name, files, silent); err == nil {
		t.Fatal("partial DDL automatically retried")
	}
}
func TestMigrationLock(t *testing.T) {
	db, name := testDatabase(t)
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	lock := "sso_" + name + "_migrations"
	var ok int
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lock).Scan(&ok); err != nil || ok != 1 {
		t.Fatal("lock unavailable")
	}
	defer conn.ExecContext(ctx, "DO RELEASE_LOCK(?)", lock)
	short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err = Apply(short, db, name, nil, silent); err == nil {
		t.Fatal("concurrent migration accepted")
	}
}
func TestEmbeddedSchemas(t *testing.T) {
	for _, group := range []string{"business", "bff"} {
		t.Run(group, func(t *testing.T) {
			db, name := testDatabase(t)
			files, err := Load(bundle.SQL, group)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err = Apply(context.Background(), db, name, files, silent); err != nil {
					t.Fatal(err)
				}
			}
			var count int
			if err = db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE checksum<>'incomplete'").Scan(&count); err != nil || count != len(files) {
				t.Fatalf("ledger mismatch: %d %v", count, err)
			}
		})
	}
}
func TestLoadRejectsUnknownTargets(t *testing.T) {
	if _, err := Load(bundle.SQL, "../business"); err == nil {
		t.Fatal("invalid target accepted")
	}
	if _, err := Load(fstest.MapFS{}, "business"); err == nil {
		t.Fatal("empty bundle accepted")
	}
}
