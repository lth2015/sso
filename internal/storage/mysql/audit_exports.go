package mysql

import (
	"context"
	"database/sql"
	"time"
)

const AuditBatchFormat = "sso.audit.batch.v1"

// Export DTOs intentionally freeze the reviewed fields; new list-model fields
// must not silently enter downloadable files or change this format version.
type ManagementExportEvent struct {
	ID         string    `json:"id"`
	Actor      string    `json:"actor_membership_id"`
	Action     string    `json:"action"`
	Target     string    `json:"target_id"`
	At         time.Time `json:"occurred_at"`
	RoleID     *string   `json:"role_id"`
	RoleCode   *string   `json:"role_code"`
	Membership *string   `json:"membership_id"`
}
type MachineExportEvent struct {
	ID          string    `json:"id"`
	Application string    `json:"application_id"`
	Principal   string    `json:"service_principal_id"`
	Credential  string    `json:"credential_id"`
	Action      string    `json:"action"`
	At          time.Time `json:"occurred_at"`
}
type AuditBatch[T any] struct {
	Format      string    `json:"format"`
	Kind        string    `json:"kind"`
	Tenant      string    `json:"tenant_id"`
	GeneratedAt time.Time `json:"generated_at"`
	Version     int64     `json:"authz_version"`
	Items       []T       `json:"items"`
	NextCursor  *string   `json:"next_cursor"`
}

func exportAuditBatch[T any](ctx context.Context, s TenantStore, kind string, limit int, after ID, read func(*sql.Tx, int) ([]T, error), id func(T) string) (result AuditBatch[T], err error) {
	result = AuditBatch[T]{Format: AuditBatchFormat, Kind: kind, Tenant: s.tenant.String(), Items: []T{}}
	if limit < 1 || limit > 100 {
		return result, ErrInvalid
	}
	table := ""
	switch kind {
	case "management":
		table = "audit_events"
	case "machine":
		table = "machine_events"
	default:
		return result, ErrInvalid
	}
	err = s.publicRead(ctx, "audit.read", func(tx *sql.Tx, av, _ int64) error {
		if !after.empty() {
			var exists int
			if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE tenant_id=? AND event_id=?", s.tenant, after).Scan(&exists); e != nil {
				return e
			}
			if exists != 1 {
				return ErrNotFound
			}
		}
		items, e := read(tx, limit+1)
		if e != nil {
			return e
		}
		if len(items) > limit {
			items = items[:limit]
			cursor := id(items[len(items)-1])
			result.NextCursor = &cursor
		}
		result.Items = items
		result.Version = av
		result.GeneratedAt = time.Now().UTC()
		return nil
	})
	return
}
func (s TenantStore) APIExportManagementAudit(ctx context.Context, limit int, after ID) (AuditBatch[ManagementExportEvent], error) {
	return exportAuditBatch(ctx, s, "management", limit, after, func(tx *sql.Tx, n int) (items []ManagementExportEvent, err error) {
		items = []ManagementExportEvent{}
		rows, e := tx.QueryContext(ctx, `SELECT LOWER(HEX(a.event_id)),LOWER(HEX(a.actor_membership_id)),a.action,LOWER(HEX(a.target_id)),a.occurred_at,LOWER(HEX(c.role_id)),c.role_code,LOWER(HEX(c.membership_id)) FROM audit_events a LEFT JOIN role_binding_changes c ON c.tenant_id=a.tenant_id AND c.id=a.target_id WHERE a.tenant_id=? AND a.event_id>? ORDER BY a.event_id LIMIT ?`, s.tenant, after, n)
		if e != nil {
			return items, e
		}
		defer rows.Close()
		for rows.Next() {
			var v ManagementExportEvent
			var role, code, member sql.NullString
			if e = rows.Scan(&v.ID, &v.Actor, &v.Action, &v.Target, &v.At, &role, &code, &member); e != nil {
				return items, e
			}
			if role.Valid {
				v.RoleID = &role.String
				v.RoleCode = &code.String
				v.Membership = &member.String
			}
			items = append(items, v)
		}
		return items, rows.Err()
	}, func(v ManagementExportEvent) string { return v.ID })
}
func (s TenantStore) APIExportMachineAudit(ctx context.Context, limit int, after ID) (AuditBatch[MachineExportEvent], error) {
	return exportAuditBatch(ctx, s, "machine", limit, after, func(tx *sql.Tx, n int) (items []MachineExportEvent, err error) {
		items = []MachineExportEvent{}
		rows, e := tx.QueryContext(ctx, `SELECT LOWER(HEX(event_id)),LOWER(HEX(application_id)),LOWER(HEX(service_principal_id)),LOWER(HEX(credential_id)),action,occurred_at FROM machine_events WHERE tenant_id=? AND event_id>? ORDER BY event_id LIMIT ?`, s.tenant, after, n)
		if e != nil {
			return items, e
		}
		defer rows.Close()
		for rows.Next() {
			var v MachineExportEvent
			if e = rows.Scan(&v.ID, &v.Application, &v.Principal, &v.Credential, &v.Action, &v.At); e != nil {
				return items, e
			}
			items = append(items, v)
		}
		return items, rows.Err()
	}, func(v MachineExportEvent) string { return v.ID })
}
