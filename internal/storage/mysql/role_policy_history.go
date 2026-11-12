package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

const RolePolicyHistoryFormat = "sso.role.policy-history.v1"

var ErrPolicyHistoryUnavailable = errors.New("role policy history unavailable")

type rolePolicyChange struct {
	Code          string
	Before, After []RolePolicy
}
type RolePolicyHistoryEntry struct {
	ID      string       `json:"id"`
	Actor   string       `json:"actor_membership_id"`
	Code    string       `json:"role_code"`
	Action  string       `json:"action"`
	At      time.Time    `json:"occurred_at"`
	Version int64        `json:"authz_version"`
	Before  []RolePolicy `json:"before"`
	After   []RolePolicy `json:"after"`
}
type RolePolicyHistoryPage struct {
	Format     string                   `json:"format"`
	Tenant     string                   `json:"tenant_id"`
	Role       string                   `json:"role_id"`
	Items      []RolePolicyHistoryEntry `json:"items"`
	NextCursor *string                  `json:"next_cursor"`
}

func canonicalRolePolicies(policies []RolePolicy) ([]RolePolicy, error) {
	if e := validatePolicies(policies); e != nil {
		return nil, e
	}
	result := append([]RolePolicy{}, policies...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Permission == result[j].Permission {
			return result[i].Effect < result[j].Effect
		}
		return result[i].Permission < result[j].Permission
	})
	return result, nil
}
func samePolicies(a, b []RolePolicy) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func normalizePolicyChange(action string, change rolePolicyChange) (rolePolicyChange, error) {
	if !roleCodePattern.MatchString(change.Code) || change.Code == "owner" {
		return rolePolicyChange{}, ErrPolicyHistoryUnavailable
	}
	before, e := canonicalRolePolicies(change.Before)
	if e != nil {
		return rolePolicyChange{}, ErrPolicyHistoryUnavailable
	}
	after, e := canonicalRolePolicies(change.After)
	if e != nil {
		return rolePolicyChange{}, ErrPolicyHistoryUnavailable
	}
	switch action {
	case "role.created":
		if len(before) != 0 {
			return rolePolicyChange{}, ErrPolicyHistoryUnavailable
		}
	case "role.updated":
		if samePolicies(before, after) {
			return rolePolicyChange{}, ErrPolicyHistoryUnavailable
		}
	case "role.deleted":
		if len(after) != 0 {
			return rolePolicyChange{}, ErrPolicyHistoryUnavailable
		}
	default:
		return rolePolicyChange{}, ErrPolicyHistoryUnavailable
	}
	return rolePolicyChange{Code: change.Code, Before: before, After: after}, nil
}

// The caller owns the role transaction and tenant lock. A snapshot failure
// aborts the original mutation, version bump, audit event and outbox together.
func (s TenantStore) recordRolePolicyChange(ctx context.Context, tx *sql.Tx, role ID, action string, version int64, change rolePolicyChange) error {
	normalized, e := normalizePolicyChange(action, change)
	if e != nil || role.empty() || version < 1 {
		return ErrPolicyHistoryUnavailable
	}
	before, e := json.Marshal(normalized.Before)
	if e != nil {
		return ErrPolicyHistoryUnavailable
	}
	after, e := json.Marshal(normalized.After)
	if e != nil {
		return ErrPolicyHistoryUnavailable
	}
	event, e := NewID()
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO audit_events(event_id,tenant_id,actor_membership_id,action,target_id) VALUES(?,?,?,?,?)`, event, s.tenant, s.actor, action, role); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO outbox_events(event_id,tenant_id,aggregate_id,aggregate_version,type) VALUES(?,?,?,?,?)`, event, s.tenant, role, version, action); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO role_policy_changes(event_id,tenant_id,role_id,role_code,authz_version,before_policies,after_policies) VALUES(?,?,?,?,?,?,?)`, event, s.tenant, role, normalized.Code, version, string(before), string(after))
	return e
}
func storedRolePolicies(raw []byte) ([]RolePolicy, error) {
	var rows []map[string]json.RawMessage
	if e := json.Unmarshal(raw, &rows); e != nil || rows == nil || len(rows) > 12 {
		return nil, ErrPolicyHistoryUnavailable
	}
	policies := make([]RolePolicy, 0, len(rows))
	for _, row := range rows {
		if len(row) != 2 || row["permission"] == nil || row["effect"] == nil {
			return nil, ErrPolicyHistoryUnavailable
		}
		var p RolePolicy
		if json.Unmarshal(row["permission"], &p.Permission) != nil || json.Unmarshal(row["effect"], &p.Effect) != nil {
			return nil, ErrPolicyHistoryUnavailable
		}
		policies = append(policies, p)
	}
	result, e := canonicalRolePolicies(policies)
	if e != nil {
		return nil, ErrPolicyHistoryUnavailable
	}
	return result, nil
}

// A nil cursor means the first page, distinct from a provided all-zero ID.
func (s TenantStore) APIRolePolicyHistory(ctx context.Context, role ID, limit int, after *ID) (RolePolicyHistoryPage, error) {
	page := RolePolicyHistoryPage{Format: RolePolicyHistoryFormat, Tenant: s.tenant.String(), Role: role.String(), Items: []RolePolicyHistoryEntry{}}
	if limit < 1 || limit > 100 {
		return RolePolicyHistoryPage{}, ErrInvalid
	}
	e := s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, av, ov int64) error {
		// Do not join a current role into the history projection: deletion and code
		// reuse cannot rewrite past rules. Both audit and snapshot namespaces bind.
		var exists int
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE tenant_id=? AND id=?) OR EXISTS(SELECT 1 FROM role_policy_changes h JOIN audit_events a ON a.event_id=h.event_id AND a.tenant_id=h.tenant_id AND a.target_id=h.role_id WHERE h.tenant_id=? AND h.role_id=?)`, s.tenant, role, s.tenant, role).Scan(&exists); e != nil {
			return e
		}
		if exists == 0 {
			return ErrNotFound
		}
		var cursor ID
		if after != nil {
			cursor = *after
			if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM role_policy_changes h JOIN audit_events a ON a.event_id=h.event_id AND a.tenant_id=h.tenant_id AND a.target_id=h.role_id WHERE h.tenant_id=? AND h.role_id=? AND h.event_id=?`, s.tenant, role, cursor).Scan(&exists); e != nil {
				return e
			}
			if exists != 1 {
				return ErrNotFound
			}
		}
		rows, e := tx.QueryContext(ctx, `SELECT LOWER(HEX(h.event_id)),LOWER(HEX(a.actor_membership_id)),h.role_code,a.action,a.occurred_at,h.authz_version,h.before_policies,h.after_policies FROM role_policy_changes h JOIN audit_events a ON a.event_id=h.event_id AND a.tenant_id=h.tenant_id AND a.target_id=h.role_id WHERE h.tenant_id=? AND h.role_id=? AND h.event_id>? ORDER BY h.event_id LIMIT ?`, s.tenant, role, cursor, limit+1)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var entry RolePolicyHistoryEntry
			var before, after []byte
			if e = rows.Scan(&entry.ID, &entry.Actor, &entry.Code, &entry.Action, &entry.At, &entry.Version, &before, &after); e != nil {
				return e
			}
			entry.Before, e = storedRolePolicies(before)
			if e != nil {
				return e
			}
			entry.After, e = storedRolePolicies(after)
			if e != nil {
				return e
			}
			normalized, e := normalizePolicyChange(entry.Action, rolePolicyChange{Code: entry.Code, Before: entry.Before, After: entry.After})
			if e != nil || entry.Version < 1 || entry.At.IsZero() {
				return ErrPolicyHistoryUnavailable
			}
			entry.Before = normalized.Before
			entry.After = normalized.After
			page.Items = append(page.Items, entry)
		}
		if e = rows.Err(); e != nil {
			return e
		}
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			cursor := page.Items[len(page.Items)-1].ID
			page.NextCursor = &cursor
		}
		return nil
	})
	if e != nil {
		return RolePolicyHistoryPage{}, e
	}
	return page, nil
}
