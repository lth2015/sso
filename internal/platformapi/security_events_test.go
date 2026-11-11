package platformapi

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"

	store "github.com/lth2015/sso/internal/storage/mysql"
)

// Runs inside the real OAuth/PKCE fixture, before the revocation-race probe.
func verifySecurityEventBoundary(t *testing.T, db *sql.DB, repo store.Store, a store.Authorization) {
	t.Helper()
	ctx := context.Background()
	foreign, err := store.NewID()
	if err != nil {
		t.Fatal("event fixture identity unavailable")
	}
	ids := make([]store.ID, 5)
	for i := range ids {
		ids[i], err = store.NewID()
		if err != nil {
			t.Fatal("event fixture ID unavailable")
		}
	}
	if bytes.Compare(ids[0][:], ids[1][:]) < 0 {
		ids[0], ids[1] = ids[1], ids[0]
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("event fixture transaction unavailable")
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO users(id) VALUES(?)`, foreign); err != nil {
		t.Fatal("event fixture identity creation failed")
	}
	for i, id := range ids {
		user, action, at := a.User, "session.authenticated", "2099-01-01 00:00:00.123456"
		if i == 2 {
			at = "2098-01-01 00:00:00.123456"
		}
		if i == 3 {
			user = foreign
		}
		if i == 4 {
			action = "private.unreviewed"
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO identity_events(id,user_id,action,target_id,occurred_at) VALUES(?,?,?,?,?)`, id, user, action, id, at); err != nil {
			t.Fatal("event fixture insertion failed")
		}
	}
	if tx.Commit() != nil {
		t.Fatal("event fixture commit failed")
	}
	defer func() {
		for _, id := range ids {
			if _, e := db.Exec(`DELETE FROM identity_events WHERE id=?`, id); e != nil {
				t.Error("event fixture cleanup failed")
			}
		}
		if _, e := db.Exec(`DELETE FROM users WHERE id=?`, foreign); e != nil {
			t.Error("event identity cleanup failed")
		}
	}()
	items, err := repo.APISecurityEvents(ctx, a, 2, store.ID{})
	if err != nil || len(items) != 2 || items[0].ID != ids[0].String() || items[1].ID != ids[1].String() {
		t.Fatal("events not scoped and chronologically ordered with stable ties")
	}
	items, err = repo.APISecurityEvents(ctx, a, 2, ids[0])
	if err != nil || len(items) != 2 || items[0].ID != ids[1].String() || items[1].ID != ids[2].String() {
		t.Fatal("event cursor skipped or repeated records")
	}
	for _, id := range ids[3:] {
		if _, err = repo.APISecurityEvents(ctx, a, 2, id); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("foreign or hidden cursor was accepted")
		}
	}
	for _, limit := range []int{0, 102} {
		if _, err = repo.APISecurityEvents(ctx, a, limit, store.ID{}); !errors.Is(err, store.ErrInvalid) {
			t.Fatal("event query limit unbounded")
		}
	}
	bad := a
	bad.User = foreign
	if _, err = repo.APISecurityEvents(ctx, bad, 2, store.ID{}); !errors.Is(err, store.ErrDenied) {
		t.Fatal("event query trusted caller user override")
	}
}
