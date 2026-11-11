package mysql

import (
	"context"
	"database/sql"
)

func (s Store) APITenant(ctx context.Context, a Authorization) (v TenantSummary, err error) {
	err = s.APITransaction(ctx, a, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT LOWER(HEX(t.id)),t.slug,LOWER(HEX(m.id)),EXISTS(SELECT 1 FROM role_bindings b JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE b.tenant_id=t.id AND b.membership_id=m.id AND r.code='owner'),t.authz_version,t.org_version,LOWER(HEX(tr.org_id)),root.name FROM tenants t JOIN memberships m ON m.tenant_id=t.id JOIN tenant_roots tr ON tr.tenant_id=t.id JOIN org_nodes root ON root.tenant_id=t.id AND root.id=tr.org_id WHERE t.id=? AND m.id=? AND m.user_id=?`, a.Tenant, a.Member, a.User).Scan(&v.ID, &v.Slug, &v.Membership, &v.Owner, &v.AuthzVersion, &v.OrgVersion, &v.Root, &v.Name)
	})
	return
}
func (s TenantStore) APIRestoreMember(ctx context.Context, id ID, expected int64) error {
	return s.publicRead(ctx, "members.write", func(tx *sql.Tx, av, ov int64) error {
		if av != expected {
			return ErrConflict
		}
		if err := s.guardMemberChange(ctx, tx, id); err != nil {
			return err
		}
		var state, userStatus string
		e := tx.QueryRowContext(ctx, `SELECT m.status,u.status FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.id=? FOR UPDATE`, s.tenant, id).Scan(&state, &userStatus)
		if e == sql.ErrNoRows {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		if state != "suspended" || userStatus != "active" {
			return ErrConflict
		}
		if _, e = tx.ExecContext(ctx, `UPDATE memberships SET status='active',security_version=security_version+1 WHERE tenant_id=? AND id=?`, s.tenant, id); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE tenants SET authz_version=authz_version+1 WHERE id=?`, s.tenant); e != nil {
			return e
		}
		return record(ctx, tx, s.tenant, s.actor, "member.restored", id, av+1)
	})
}
