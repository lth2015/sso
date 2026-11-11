package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrProtectedOrganization = errors.New("protected_organization")
var ErrOrganizationNotEmpty = errors.New("organization_not_empty")

// A directory name changes presentation, never grants. Root names also name
// the space, so space renaming is deliberately outside organization delegation.
func (s TenantStore) RenameOrganization(ctx context.Context, org ID, name string, expected int64) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 128 || !utf8.ValidString(name) || expected < 1 {
		return 0, ErrInvalid
	}
	s = s.withRecentMFA()
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	_, version, err := s.lockPermission(ctx, tx, "organizations.write")
	if err != nil {
		return 0, err
	}
	if version != expected {
		return 0, ErrConflict
	}
	var old string
	var parent []byte
	err = tx.QueryRowContext(ctx, `SELECT name,parent_id FROM org_nodes WHERE tenant_id=? AND id=?`, s.tenant, org).Scan(&old, &parent)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if parent == nil {
		return 0, ErrProtectedOrganization
	}
	if old == name {
		return version, tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE org_nodes SET name=? WHERE tenant_id=? AND id=?`, name, s.tenant, org); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenants SET org_version=org_version+1 WHERE id=?`, s.tenant); err != nil {
		return 0, err
	}
	if err = record(ctx, tx, s.tenant, s.actor, "org.renamed", org, version+1); err != nil {
		return 0, err
	}
	return version + 1, tx.Commit()
}

// Empty-leaf deletion is serialized with create/move/association writers by
// the tenant lock. Never cascade children or even suspended-member references.
func (s TenantStore) DeleteOrganization(ctx context.Context, org ID, expected int64) (int64, error) {
	if expected < 1 {
		return 0, ErrInvalid
	}
	s = s.withRecentMFA()
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	_, version, err := s.lockPermission(ctx, tx, "organizations.write")
	if err != nil {
		return 0, err
	}
	if version != expected {
		return 0, ErrConflict
	}
	var parent []byte
	err = tx.QueryRowContext(ctx, `SELECT parent_id FROM org_nodes WHERE tenant_id=? AND id=?`, s.tenant, org).Scan(&parent)
	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if parent == nil {
		return 0, ErrProtectedOrganization
	}
	var occupied bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM org_nodes WHERE tenant_id=? AND parent_id=?) OR EXISTS(SELECT 1 FROM org_memberships WHERE tenant_id=? AND org_id=?)`, s.tenant, org, s.tenant, org).Scan(&occupied)
	if err != nil {
		return 0, err
	}
	if occupied {
		return 0, ErrOrganizationNotEmpty
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM org_closure WHERE tenant_id=? AND (ancestor_id=? OR descendant_id=?)`, s.tenant, org, org); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM org_nodes WHERE tenant_id=? AND id=?`, s.tenant, org); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenants SET org_version=org_version+1,authz_version=authz_version+1 WHERE id=?`, s.tenant); err != nil {
		return 0, err
	}
	if err = record(ctx, tx, s.tenant, s.actor, "org.deleted", org, version+1); err != nil {
		return 0, err
	}
	return version + 1, tx.Commit()
}

func (s TenantStore) APIOrganizationMembers(ctx context.Context, org ID, limit int, after ID) (items []MemberSummary, version int64, err error) {
	items = []MemberSummary{}
	if limit < 1 || limit > 101 {
		return items, 0, ErrInvalid
	}
	err = s.publicRead(ctx, "organizations.read", func(tx *sql.Tx, av, _ int64) error {
		// A structural read permission never implies the ability to enumerate people
		// or their verified aliases. Require both effective permissions, deny first.
		allowed, _, e := s.permission(ctx, tx, s.actor, "members.read")
		if e != nil {
			return e
		}
		if !allowed {
			return ErrDenied
		}
		var found ID
		e = tx.QueryRowContext(ctx, `SELECT id FROM org_nodes WHERE tenant_id=? AND id=?`, s.tenant, org).Scan(&found)
		if e == sql.ErrNoRows {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		version = av
		rows, e := tx.QueryContext(ctx, `SELECT LOWER(HEX(m.id)),LOWER(HEX(m.user_id)),m.status,EXISTS(SELECT 1 FROM role_bindings b JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE b.tenant_id=m.tenant_id AND b.membership_id=m.id AND r.code='owner'),(SELECT normalized_email FROM login_aliases a WHERE a.user_id=m.user_id AND a.verified_at IS NOT NULL ORDER BY normalized_email LIMIT 1) FROM org_memberships om JOIN memberships m ON m.tenant_id=om.tenant_id AND m.id=om.membership_id WHERE om.tenant_id=? AND om.org_id=? AND m.id>? ORDER BY m.id LIMIT ?`, s.tenant, org, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v MemberSummary
			var email sql.NullString
			if e = rows.Scan(&v.ID, &v.User, &v.Status, &v.Owner, &email); e != nil {
				return e
			}
			if email.Valid {
				v.Email = &email.String
			}
			items = append(items, v)
		}
		return rows.Err()
	})
	return
}

// Removal changes only the organization association. Membership, owner and
// role bindings remain intact. Inactive memberships must be removable as well.
func (s TenantStore) UnassignOrganization(ctx context.Context, member, org ID, expected int64) error {
	if expected < 1 {
		return ErrInvalid
	}
	s = s.withRecentMFA()
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	version, _, err := s.lockPermission(ctx, tx, "organizations.write")
	if err != nil {
		return err
	}
	if version != expected {
		return ErrConflict
	}
	var found ID
	for _, lookup := range []struct {
		query string
		id    ID
	}{
		{`SELECT id FROM org_nodes WHERE tenant_id=? AND id=?`, org},
		{`SELECT id FROM memberships WHERE tenant_id=? AND id=?`, member},
	} {
		err = tx.QueryRowContext(ctx, lookup.query, s.tenant, lookup.id).Scan(&found)
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM org_memberships WHERE tenant_id=? AND org_id=? AND membership_id=?`, s.tenant, org, member)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenants SET authz_version=authz_version+1 WHERE id=?`, s.tenant); err != nil {
		return err
	}
	if err = record(ctx, tx, s.tenant, s.actor, "org.member_unassigned", member, version+1); err != nil {
		return err
	}
	return tx.Commit()
}

func (s TenantStore) withRecentMFA() TenantStore {
	if s.authority != nil {
		copy := *s.authority
		copy.Recent = true
		s.authority = &copy
	}
	return s
}
