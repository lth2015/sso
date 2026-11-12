package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

const ApplicationAccessHistoryFormat = "sso.application.user-access-history.v1"

var ErrApplicationAccessHistoryUnavailable = errors.New("application access history unavailable")

type ApplicationAccessHistoryEntry struct {
	ID      string    `json:"id"`
	Actor   string    `json:"actor_membership_id"`
	Member  *string   `json:"membership_id"`
	Action  string    `json:"action"`
	At      time.Time `json:"occurred_at"`
	Mode    string    `json:"mode"`
	Version string    `json:"version"`
}
type ApplicationAccessHistoryPage struct {
	Format      string                          `json:"format"`
	Tenant      string                          `json:"tenant_id"`
	Application string                          `json:"application_id"`
	Items       []ApplicationAccessHistoryEntry `json:"items"`
	Next        *string                         `json:"next_cursor"`
}

func validAccessHistoryEntry(v ApplicationAccessHistoryEntry, revision int64) bool {
	event, e := ParseID(v.ID)
	actor, a := ParseID(v.Actor)
	if e != nil || a != nil || event.empty() || actor.empty() || revision < 1 || v.At.IsZero() || !validUserAccessMode(v.Mode) {
		return false
	}
	switch v.Action {
	case "application.user_access.mode_changed":
		return v.Member == nil
	case "application.user_access.granted", "application.user_access.revoked":
		if v.Member == nil {
			return false
		}
		member, e := ParseID(*v.Member)
		return e == nil && !member.empty()
	default:
		return false
	}
}

func (s TenantStore) APIApplicationAccessHistory(ctx context.Context, app ID, limit int, after *ID) (ApplicationAccessHistoryPage, error) {
	page := ApplicationAccessHistoryPage{Format: ApplicationAccessHistoryFormat, Tenant: s.tenant.String(), Application: app.String(), Items: []ApplicationAccessHistoryEntry{}}
	if limit < 1 || limit > 100 {
		return ApplicationAccessHistoryPage{}, ErrInvalid
	}
	err := s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if e := s.ownedApplication(ctx, tx, app, false); e != nil {
			return e
		}
		var cursor ID
		if after != nil {
			cursor = *after
			var exists int
			if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM application_user_access_changes h JOIN audit_events a ON a.event_id=h.event_id AND a.tenant_id=h.tenant_id AND a.target_id=h.application_id WHERE h.tenant_id=? AND h.application_id=? AND h.event_id=?`, s.tenant, app, cursor).Scan(&exists); e != nil {
				return e
			}
			if exists != 1 {
				return ErrNotFound
			}
		}
		// Live member metadata and a potentially consumed outbox cannot rewrite history.
		// Cross-linked audit data fails the whole page rather than disclosing it.
		rows, e := tx.QueryContext(ctx, `SELECT LOWER(HEX(h.event_id)),LOWER(HEX(a.actor_membership_id)),LOWER(HEX(h.membership_id)),a.action,a.occurred_at,h.mode,h.version,(a.tenant_id=h.tenant_id AND a.target_id=h.application_id) FROM application_user_access_changes h JOIN audit_events a ON a.event_id=h.event_id WHERE h.tenant_id=? AND h.application_id=? AND h.event_id>? ORDER BY h.event_id LIMIT ?`, s.tenant, app, cursor, limit+1)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var entry ApplicationAccessHistoryEntry
			var member sql.NullString
			var revision int64
			var namespace bool
			if e = rows.Scan(&entry.ID, &entry.Actor, &member, &entry.Action, &entry.At, &entry.Mode, &revision, &namespace); e != nil {
				return e
			}
			if member.Valid {
				v := member.String
				entry.Member = &v
			}
			if !namespace || !validAccessHistoryEntry(entry, revision) {
				return ErrApplicationAccessHistoryUnavailable
			}
			entry.Version = strconv.FormatInt(revision, 10)
			page.Items = append(page.Items, entry)
		}
		if e = rows.Err(); e != nil {
			return e
		}
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			id := page.Items[len(page.Items)-1].ID
			page.Next = &id
		}
		return nil
	})
	if err != nil {
		return ApplicationAccessHistoryPage{}, err
	}
	return page, nil
}
