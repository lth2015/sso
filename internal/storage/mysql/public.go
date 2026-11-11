package mysql

import (
	"context"
	"database/sql"
	"time"
)

type TenantSummary struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Membership   string `json:"membership_id"`
	Owner        bool   `json:"owner"`
	AuthzVersion int64  `json:"authz_version"`
	OrgVersion   int64  `json:"org_version"`
	Root         string `json:"root_id"`
	Name         string `json:"name"`
}
type MemberSummary struct {
	ID     string  `json:"id"`
	User   string  `json:"user_id"`
	Status string  `json:"status"`
	Owner  bool    `json:"owner"`
	Email  *string `json:"email"`
}
type InvitationSummary struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Expires int64  `json:"expires_at"`
	State   string `json:"state"`
}
type AuditSummary struct {
	ID         string    `json:"id"`
	Actor      string    `json:"actor_membership_id"`
	Action     string    `json:"action"`
	Target     string    `json:"target_id"`
	At         time.Time `json:"occurred_at"`
	RoleID     *string   `json:"role_id,omitempty"`
	RoleCode   *string   `json:"role_code,omitempty"`
	Membership *string   `json:"membership_id,omitempty"`
}
type SessionSummary struct {
	ID       string `json:"id"`
	Current  bool   `json:"current"`
	Active   bool   `json:"active"`
	AuthTime int64  `json:"authenticated_at"`
	Method   string `json:"second_factor"`
	Idle     int64  `json:"idle_expires_at"`
	Absolute int64  `json:"absolute_expires_at"`
}

func (s Store) APITenants(ctx context.Context, a Authorization, limit int, after ID) (items []TenantSummary, err error) {
	items = []TenantSummary{}
	err = s.APITransaction(ctx, a, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT LOWER(HEX(t.id)),t.slug,LOWER(HEX(m.id)),EXISTS(SELECT 1 FROM role_bindings b JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE b.tenant_id=t.id AND b.membership_id=m.id AND r.code='owner'),t.authz_version,t.org_version,LOWER(HEX(tr.org_id)),root.name FROM tenants t JOIN memberships m ON m.tenant_id=t.id JOIN tenant_roots tr ON tr.tenant_id=t.id JOIN org_nodes root ON root.tenant_id=t.id AND root.id=tr.org_id WHERE m.user_id=? AND m.status='active' AND t.status='active' AND t.id>? ORDER BY t.id LIMIT ?`, a.User, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v TenantSummary
			if e = rows.Scan(&v.ID, &v.Slug, &v.Membership, &v.Owner, &v.AuthzVersion, &v.OrgVersion, &v.Root, &v.Name); e != nil {
				return e
			}
			items = append(items, v)
		}
		return rows.Err()
	})
	return
}
func (s Store) APIBootstrap(ctx context.Context, a Authorization, slug, name string) (result Bootstrap, err error) {
	if !slugPattern.MatchString(slug) || len(name) == 0 || len(name) > 128 {
		return result, ErrInvalid
	}
	err = s.APITransaction(ctx, a, func(tx *sql.Tx) error {
		// Serialize a user's tenant creation quota against concurrent API calls.
		var id ID
		if e := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=? FOR UPDATE`, a.User).Scan(&id); e != nil {
			return e
		}
		var count int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships m JOIN role_bindings b ON b.tenant_id=m.tenant_id AND b.membership_id=m.id JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE m.user_id=? AND m.status='active' AND r.code='owner'`, a.User).Scan(&count); e != nil {
			return e
		}
		if count >= 20 {
			return ErrInvalid
		}
		var e error
		result, e = bootstrapTenantTx(ctx, tx, a.User, slug, name)
		if e != nil {
			return e
		}
		// The first-party platform is enabled as part of its own tenant bootstrap.
		_, e = tx.ExecContext(ctx, `INSERT INTO tenant_applications(tenant_id,application_id) SELECT ?,id FROM applications WHERE code='local-platform' AND active=TRUE`, result.Tenant)
		return e
	})
	return
}
func (s TenantStore) publicRead(ctx context.Context, permission string, run func(*sql.Tx, int64, int64) error) error {
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	av, ov, err := s.lockPermission(ctx, tx, permission)
	if err != nil {
		return err
	}
	if err = run(tx, av, ov); err != nil {
		return err
	}
	return tx.Commit()
}
func (s TenantStore) APIMembers(ctx context.Context, limit int, after ID) (items []MemberSummary, version int64, err error) {
	items = []MemberSummary{}
	err = s.publicRead(ctx, "members.read", func(tx *sql.Tx, av, ov int64) error {
		version = av
		rows, e := tx.QueryContext(ctx, `SELECT LOWER(HEX(m.id)),LOWER(HEX(m.user_id)),m.status,EXISTS(SELECT 1 FROM role_bindings b JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE b.tenant_id=m.tenant_id AND b.membership_id=m.id AND r.code='owner'),(SELECT normalized_email FROM login_aliases a WHERE a.user_id=m.user_id AND a.verified_at IS NOT NULL ORDER BY normalized_email LIMIT 1) FROM memberships m WHERE m.tenant_id=? AND m.id>? ORDER BY m.id LIMIT ?`, s.tenant, after, limit)
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
func (s TenantStore) APIOrganizations(ctx context.Context, limit int, after ID) (items []Org, version int64, err error) {
	items = []Org{}
	err = s.publicRead(ctx, "organizations.read", func(tx *sql.Tx, av, ov int64) error {
		version = ov
		rows, e := tx.QueryContext(ctx, `SELECT id,parent_id,name FROM org_nodes WHERE tenant_id=? AND id>? ORDER BY id LIMIT ?`, s.tenant, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v Org
			var p []byte
			if e = rows.Scan(&v.ID, &p, &v.Name); e != nil {
				return e
			}
			if p != nil {
				var id ID
				if e = id.Scan(p); e != nil {
					return e
				}
				v.Parent = &id
			}
			items = append(items, v)
		}
		return rows.Err()
	})
	return
}
func (s TenantStore) APIInvitations(ctx context.Context, limit int, after ID) (items []InvitationSummary, err error) {
	items = []InvitationSummary{}
	err = s.publicRead(ctx, "invitations.manage", func(tx *sql.Tx, av, ov int64) error {
		rows, e := tx.QueryContext(ctx, `SELECT LOWER(HEX(id)),target_email,expires_at,IF(consumed_at IS NOT NULL,'closed',IF(expires_at<=?,'expired','pending')) FROM invitations WHERE tenant_id=? AND id>? ORDER BY id LIMIT ?`, time.Now().Unix(), s.tenant, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v InvitationSummary
			if e = rows.Scan(&v.ID, &v.Email, &v.Expires, &v.State); e != nil {
				return e
			}
			items = append(items, v)
		}
		return rows.Err()
	})
	return
}
func (s TenantStore) APIRevokeInvitation(ctx context.Context, id ID) error {
	return s.publicRead(ctx, "invitations.manage", func(tx *sql.Tx, av, ov int64) error {
		var consumed sql.NullTime
		e := tx.QueryRowContext(ctx, `SELECT consumed_at FROM invitations WHERE tenant_id=? AND id=? FOR UPDATE`, s.tenant, id).Scan(&consumed)
		if e == sql.ErrNoRows {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		if consumed.Valid {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `UPDATE invitations SET consumed_at=UTC_TIMESTAMP(6) WHERE tenant_id=? AND id=?`, s.tenant, id); e != nil {
			return e
		}
		revision, e := bumpInvitationRevision(ctx, tx, id)
		if e != nil {
			return e
		}
		return record(ctx, tx, s.tenant, s.actor, "invitation.revoked", id, revision)
	})
}
func (s TenantStore) APIAudit(ctx context.Context, limit int, after ID) (items []AuditSummary, err error) {
	items = []AuditSummary{}
	err = s.publicRead(ctx, "audit.read", func(tx *sql.Tx, av, ov int64) error {
		rows, e := tx.QueryContext(ctx, `SELECT LOWER(HEX(a.event_id)),LOWER(HEX(a.actor_membership_id)),a.action,LOWER(HEX(a.target_id)),a.occurred_at,LOWER(HEX(c.role_id)),c.role_code,LOWER(HEX(c.membership_id)) FROM audit_events a LEFT JOIN role_binding_changes c ON c.tenant_id=a.tenant_id AND c.id=a.target_id WHERE a.tenant_id=? AND a.event_id>? ORDER BY a.event_id LIMIT ?`, s.tenant, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v AuditSummary
			var roleID, roleCode, member sql.NullString
			if e = rows.Scan(&v.ID, &v.Actor, &v.Action, &v.Target, &v.At, &roleID, &roleCode, &member); e != nil {
				return e
			}
			if roleID.Valid {
				v.RoleID = &roleID.String
				v.RoleCode = &roleCode.String
				v.Membership = &member.String
			}
			items = append(items, v)
		}
		return rows.Err()
	})
	return
}
func (s Store) APISessions(ctx context.Context, a Authorization, limit int, after ID) (items []SessionSummary, err error) {
	items = []SessionSummary{}
	err = s.APITransaction(ctx, a, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, `SELECT id,active AND user_version=(SELECT security_version FROM users WHERE id=?) AND idle_expires_at>? AND absolute_expires_at>?,auth_time,amr,idle_expires_at,absolute_expires_at FROM sso_sessions WHERE user_id=? AND id>? ORDER BY id LIMIT ?`, a.User, time.Now().Unix(), time.Now().Unix(), a.User, after, limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var id ID
			var v SessionSummary
			if e = rows.Scan(&id, &v.Active, &v.AuthTime, &v.Method, &v.Idle, &v.Absolute); e != nil {
				return e
			}
			v.ID = id.String()
			v.Current = id == a.Session
			items = append(items, v)
		}
		return rows.Err()
	})
	return
}
func (s Store) APIRevokeSession(ctx context.Context, a Authorization, id ID) error {
	return s.APITransaction(ctx, a, func(tx *sql.Tx) error {
		var active bool
		e := tx.QueryRowContext(ctx, `SELECT active FROM sso_sessions WHERE id=? AND user_id=? FOR UPDATE`, id, a.User).Scan(&active)
		if e == sql.ErrNoRows {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		if !active {
			return nil
		}
		if _, e = tx.ExecContext(ctx, `INSERT IGNORE INTO protocol_logout_jobs(sid,user_id) SELECT DISTINCT hydra_sid,user_id FROM business_grants WHERE session_id=? AND user_id=? AND hydra_sid<>''`, id, a.User); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE sso_sessions SET active=FALSE WHERE id=? AND user_id=?`, id, a.User); e != nil {
			return e
		}
		return identityEvent(ctx, tx, a.User, "session.revoked", id)
	})
}
