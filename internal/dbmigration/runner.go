// Package dbmigration applies checksummed SQL on one locked MySQL connection.
package dbmigration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	driver "github.com/go-sql-driver/mysql"
	"io/fs"
	"regexp"
	"sort"
	"time"
)

type Migration struct{ Version, SQL, Checksum string }

var versionName = regexp.MustCompile(`^[0-9]{3}_[a-z0-9_]+$`)

func Load(source fs.FS, group string) ([]Migration, error) {
	if group != "business" && group != "bff" {
		return nil, errors.New("unknown migration group")
	}
	paths, err := fs.Glob(source, group+"/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, errors.New("no migrations found")
	}
	result := make([]Migration, 0, len(paths))
	for _, path := range paths {
		version := path[len(group)+1 : len(path)-4]
		if !versionName.MatchString(version) {
			return nil, errors.New("invalid migration filename")
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return nil, err
		}
		result = append(result, Migration{version, string(data), fmt.Sprintf("%x", sha256.Sum256(data))})
	}
	return result, nil
}

// Apply deliberately withholds driver errors: SQL or connection details must not enter CI logs.
// MySQL DDL is not transactional. An interrupted migration must be inspected before retrying.
func Apply(ctx context.Context, db *sql.DB, database string, migrations []Migration, report func(string)) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.New("migration connection unavailable")
	}
	defer conn.Close()
	var locked sql.NullInt64
	lock := "sso_" + database + "_migrations"
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 10)", lock).Scan(&locked); err != nil || !locked.Valid || locked.Int64 != 1 {
		return errors.New("migration lock unavailable")
	}
	// Close the dedicated connection to release the advisory lock, even after cancellation.
	defer func() {
		release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		conn.ExecContext(release, "DO RELEASE_LOCK(?)", lock)
	}()
	if _, err = conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version VARCHAR(64) PRIMARY KEY, checksum CHAR(64) NOT NULL)"); err != nil {
		return operationError("migration ledger creation", err)
	}
	applied := map[string]string{}
	rows, err := conn.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
	if err != nil {
		return operationError("migration ledger read", err)
	}
	for rows.Next() {
		var v, c string
		if err = rows.Scan(&v, &c); err != nil {
			rows.Close()
			return errors.New("invalid migration ledger")
		}
		applied[v] = c
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return operationError("migration ledger read", err)
	}
	known := map[string]bool{}
	for _, m := range migrations {
		known[m.Version] = true
		if checksum, ok := applied[m.Version]; ok && checksum != m.Checksum {
			return fmt.Errorf("migration checksum mismatch: %s", m.Version)
		}
	}
	for version := range applied {
		if !known[version] {
			return errors.New("database has migrations absent from this release; downgrade refused")
		}
	}
	for _, m := range migrations {
		if _, ok := applied[m.Version]; ok {
			report("Skipped " + m.Version)
			continue
		}
		// Mark an incomplete attempt before DDL. A retry fails closed until an operator verifies it.
		if _, err = conn.ExecContext(ctx, "INSERT INTO schema_migrations(version, checksum) VALUES (?, ?)", m.Version, "incomplete"); err != nil {
			return operationError("migration attempt recording", err)
		}
		if _, err = conn.ExecContext(ctx, m.SQL); err != nil {
			return fmt.Errorf("migration failed; inspect partial DDL before retry: %s", m.Version)
		}
		if _, err = conn.ExecContext(ctx, "UPDATE schema_migrations SET checksum=? WHERE version=?", m.Checksum, m.Version); err != nil {
			return operationError("migration completion recording", err)
		}
		report("Applied " + m.Version)
	}
	return nil
}

func operationError(stage string, err error) error {
	var mysqlError *driver.MySQLError
	if errors.As(err, &mysqlError) {
		switch mysqlError.Number {
		case 1044, 1045, 1142, 1143:
			return fmt.Errorf("database permission denied during %s (MySQL %d)", stage, mysqlError.Number)
		}
	}
	return errors.New(stage + " failed")
}
