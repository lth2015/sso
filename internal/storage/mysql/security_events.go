package mysql

import (
	"context"
	"database/sql"
	"time"
)

// Only reviewed event kinds are exposed. Targets can identify private challenges
// or management actions and must never cross the public API boundary.
const visibleSecurityEvents = `action IN ('account.registered','account.reset_requested','account.password_reset','mfa.enrolled','session.authenticated','session.logout_all','authorization.rejected','session.revoked','session.others_revoked','mfa.recovery_regenerated','mfa.replacement_started','mfa.replacement_cancelled','mfa.authenticator_replaced')`

type SecurityEventSummary struct {
	ID     string    `json:"id"`
	Action string    `json:"action"`
	At     time.Time `json:"occurred_at"`
}

func (s Store) APISecurityEvents(ctx context.Context, a Authorization, limit int, after ID) (items []SecurityEventSummary, err error) {
	items = []SecurityEventSummary{}
	if limit < 1 || limit > 101 {
		return items, ErrInvalid
	}
	err = s.APITransaction(ctx, a, func(tx *sql.Tx) error {
		query := `SELECT LOWER(HEX(id)),action,occurred_at FROM identity_events WHERE user_id=? AND ` + visibleSecurityEvents
		args := []any{a.User}
		if !after.empty() {
			var at time.Time
			e := tx.QueryRowContext(ctx, `SELECT occurred_at FROM identity_events WHERE user_id=? AND id=? AND `+visibleSecurityEvents, a.User, after).Scan(&at)
			if e == sql.ErrNoRows {
				return ErrNotFound
			}
			if e != nil {
				return e
			}
			query += ` AND (occurred_at<? OR (occurred_at=? AND id<?))`
			args = append(args, at, at, after)
		}
		query += ` ORDER BY occurred_at DESC,id DESC LIMIT ?`
		args = append(args, limit)
		rows, e := tx.QueryContext(ctx, query, args...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var item SecurityEventSummary
			if e = rows.Scan(&item.ID, &item.Action, &item.At); e != nil {
				return e
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return
}
