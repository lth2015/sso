package mysql

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"sort"
)

var ErrReservedRole = errors.New("reserved_role")
var ErrRoleInUse = errors.New("role_in_use")
var roleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
var DelegatablePermissions = []string{"audit.read", "invitations.manage", "members.read", "members.write", "organizations.read", "organizations.write"}

const RoleManagement = "roles.manage"

type RolePolicy struct {
	Permission string `json:"permission"`
	Effect     string `json:"effect"`
}
type RoleSummary struct {
	ID           string       `json:"id"`
	Code         string       `json:"code"`
	BuiltIn      bool         `json:"built_in"`
	Scope        string       `json:"scope"`
	Policies     []RolePolicy `json:"policies"`
	BindingCount int          `json:"binding_count"`
}
type PermissionSnapshot struct {
	Permissions []string `json:"permissions"`
	Denied      []string `json:"denied"`
	Owner       bool     `json:"owner"`
	Version     int64    `json:"authz_version"`
	Catalog     []string `json:"catalog"`
}

func validatePolicies(policies []RolePolicy) error {
	if len(policies) > 12 {
		return ErrInvalid
	}
	seen := map[RolePolicy]bool{}
	for _, policy := range policies {
		valid := false
		for _, name := range DelegatablePermissions {
			if policy.Permission == name {
				valid = true
			}
		}
		if !valid || (policy.Effect != "allow" && policy.Effect != "deny") || seen[policy] {
			return ErrInvalid
		}
		seen[policy] = true
	}
	return nil
}
func (s TenantStore) isOwner(ctx context.Context, tx *sql.Tx, member ID) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships m JOIN users u ON u.id=m.user_id JOIN role_bindings b ON b.tenant_id=m.tenant_id AND b.membership_id=m.id JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE m.tenant_id=? AND m.id=? AND m.status='active' AND u.status='active' AND r.code='owner'`, s.tenant, member).Scan(&count)
	return count == 1, err
}
func (s TenantStore) permission(ctx context.Context, tx *sql.Tx, member ID, name string) (bool, bool, error) {
	owner, err := s.isOwner(ctx, tx, member)
	if err != nil {
		return false, false, err
	}
	// Reserved owner administration is the recovery boundary, never a custom rule.
	if name == RoleManagement {
		return owner, false, nil
	}
	var allowed, denied int
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(p.effect='allow'),0),COALESCE(SUM(p.effect='deny'),0) FROM role_bindings b JOIN role_policies p ON p.tenant_id=b.tenant_id AND p.role_id=b.role_id WHERE b.tenant_id=? AND b.membership_id=? AND p.permission=?`, s.tenant, member, name).Scan(&allowed, &denied)
	return denied == 0 && (owner || allowed > 0), denied > 0, err
}
func (s TenantStore) lockPermission(ctx context.Context, tx *sql.Tx, name string) (int64, int64, error) {
	if s.tenant.empty() || s.actor.empty() {
		return 0, 0, ErrDenied
	}
	var av, ov int64
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status,authz_version,org_version FROM tenants WHERE id=? FOR UPDATE`, s.tenant).Scan(&status, &av, &ov)
	if err == sql.ErrNoRows {
		return 0, 0, ErrNotFound
	}
	if err != nil {
		return 0, 0, err
	}
	if status != "active" {
		return 0, 0, ErrDenied
	}
	if s.authority != nil {
		if s.authority.Tenant != s.tenant || s.authority.Member != s.actor {
			return 0, 0, ErrDenied
		}
		if err = lockAuthorization(ctx, tx, *s.authority); err != nil {
			return 0, 0, err
		}
	}
	var active ID
	err = tx.QueryRowContext(ctx, `SELECT m.id FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.id=? AND m.status='active' AND u.status='active' FOR SHARE`, s.tenant, s.actor).Scan(&active)
	if err == sql.ErrNoRows {
		return 0, 0, ErrDenied
	}
	if err != nil {
		return 0, 0, err
	}
	allowed, _, err := s.permission(ctx, tx, s.actor, name)
	if err != nil {
		return 0, 0, err
	}
	if !allowed {
		return 0, 0, ErrDenied
	}
	return av, ov, nil
}
func (s TenantStore) guardMemberChange(ctx context.Context, tx *sql.Tx, target ID) error {
	owner, err := s.isOwner(ctx, tx, s.actor)
	if err != nil {
		return err
	}
	if owner {
		return nil
	}
	// A delegated operator cannot disable or restore any reserved owner binding,
	// including an owner whose membership is currently suspended.
	var count int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM role_bindings b JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE b.tenant_id=? AND b.membership_id=? AND r.code='owner'`, s.tenant, target).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrDenied
	}
	return nil
}
func (s TenantStore) APIPermissions(ctx context.Context) (snapshot PermissionSnapshot, err error) {
	snapshot.Permissions = []string{}
	snapshot.Denied = []string{}
	snapshot.Catalog = append([]string{}, DelegatablePermissions...)
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return snapshot, err
	}
	defer tx.Rollback()
	// Any current member may inspect only their own effective permission set.
	var status string
	err = tx.QueryRowContext(ctx, `SELECT authz_version,status FROM tenants WHERE id=? FOR UPDATE`, s.tenant).Scan(&snapshot.Version, &status)
	if err != nil {
		return snapshot, err
	}
	if status != "active" {
		return snapshot, ErrDenied
	}
	if s.authority == nil {
		return snapshot, ErrDenied
	}
	if err = lockAuthorization(ctx, tx, *s.authority); err != nil {
		return snapshot, err
	}
	snapshot.Owner, err = s.isOwner(ctx, tx, s.actor)
	if err != nil {
		return snapshot, err
	}
	for _, name := range append(append([]string{}, DelegatablePermissions...), RoleManagement) {
		allowed, denied, e := s.permission(ctx, tx, s.actor, name)
		if e != nil {
			return snapshot, e
		}
		if allowed {
			snapshot.Permissions = append(snapshot.Permissions, name)
		}
		if denied {
			snapshot.Denied = append(snapshot.Denied, name)
		}
	}
	return snapshot, tx.Commit()
}
func (s TenantStore) role(ctx context.Context, tx *sql.Tx, id ID) (v RoleSummary, err error) {
	v.Policies = []RolePolicy{}
	v.Scope = "tenant"
	err = tx.QueryRowContext(ctx, `SELECT LOWER(HEX(r.id)),r.code,(SELECT COUNT(*) FROM role_bindings b WHERE b.tenant_id=r.tenant_id AND b.role_id=r.id) FROM roles r WHERE r.tenant_id=? AND r.id=?`, s.tenant, id).Scan(&v.ID, &v.Code, &v.BindingCount)
	if err == sql.ErrNoRows {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.BuiltIn = v.Code == "owner"
	rows, err := tx.QueryContext(ctx, `SELECT permission,effect FROM role_policies WHERE tenant_id=? AND role_id=? ORDER BY permission,effect`, s.tenant, id)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var p RolePolicy
		if err = rows.Scan(&p.Permission, &p.Effect); err != nil {
			return v, err
		}
		v.Policies = append(v.Policies, p)
	}
	return v, rows.Err()
}
func (s TenantStore) APIRoles(ctx context.Context, limit int, after ID) (items []RoleSummary, version int64, err error) {
	items = []RoleSummary{}
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, av, ov int64) error {
		version = av
		rows, e := tx.QueryContext(ctx, `SELECT id FROM roles WHERE tenant_id=? AND id>? ORDER BY id LIMIT ?`, s.tenant, after, limit)
		if e != nil {
			return e
		}
		ids := []ID{}
		for rows.Next() {
			var id ID
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, id := range ids {
			v, e := s.role(ctx, tx, id)
			if e != nil {
				return e
			}
			items = append(items, v)
		}
		return nil
	})
	return
}
func (s TenantStore) roleWrite(ctx context.Context, expected int64, run func(*sql.Tx) (ID, string, bool, error)) error {
	return s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, av, ov int64) error {
		if av != expected {
			return ErrConflict
		}
		id, action, changed, err := run(tx)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		if _, err = tx.ExecContext(ctx, `UPDATE tenants SET authz_version=authz_version+1 WHERE id=?`, s.tenant); err != nil {
			return err
		}
		return record(ctx, tx, s.tenant, s.actor, action, id, av+1)
	})
}
func (s TenantStore) APICreateRole(ctx context.Context, code string, policies []RolePolicy, expected int64) (id ID, err error) {
	if !roleCodePattern.MatchString(code) || code == "owner" {
		return id, ErrInvalid
	}
	if err = validatePolicies(policies); err != nil {
		return id, err
	}
	id, err = NewID()
	if err != nil {
		return id, err
	}
	err = s.roleWrite(ctx, expected, func(tx *sql.Tx) (ID, string, bool, error) {
		var count int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM roles WHERE tenant_id=?`, s.tenant).Scan(&count); e != nil {
			return id, "", false, e
		}
		if count >= 100 {
			return id, "", false, ErrInvalid
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO roles(id,tenant_id,code) VALUES(?,?,?)`, id, s.tenant, code); e != nil {
			return id, "", false, e
		}
		if e := s.writePolicies(ctx, tx, id, policies); e != nil {
			return id, "", false, e
		}
		return id, "role.created", true, nil
	})
	return
}
func (s TenantStore) writePolicies(ctx context.Context, tx *sql.Tx, id ID, policies []RolePolicy) error {
	for _, p := range policies {
		if _, err := tx.ExecContext(ctx, `INSERT INTO role_policies(tenant_id,role_id,permission,effect) VALUES(?,?,?,?)`, s.tenant, id, p.Permission, p.Effect); err != nil {
			return err
		}
	}
	return nil
}
func (s TenantStore) APIReplaceRole(ctx context.Context, id ID, policies []RolePolicy, expected int64) error {
	if err := validatePolicies(policies); err != nil {
		return err
	}
	return s.roleWrite(ctx, expected, func(tx *sql.Tx) (ID, string, bool, error) {
		current, err := s.role(ctx, tx, id)
		if err != nil {
			return id, "", false, err
		}
		if current.BuiltIn {
			return id, "", false, ErrReservedRole
		}
		wanted := append([]RolePolicy{}, policies...)
		sort.Slice(wanted, func(i, j int) bool {
			if wanted[i].Permission == wanted[j].Permission {
				return wanted[i].Effect < wanted[j].Effect
			}
			return wanted[i].Permission < wanted[j].Permission
		})
		equal := len(wanted) == len(current.Policies)
		if equal {
			for i := range wanted {
				if wanted[i] != current.Policies[i] {
					equal = false
				}
			}
		}
		if equal {
			return id, "", false, nil
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM role_policies WHERE tenant_id=? AND role_id=?`, s.tenant, id); err != nil {
			return id, "", false, err
		}
		if err = s.writePolicies(ctx, tx, id, policies); err != nil {
			return id, "", false, err
		}
		return id, "role.updated", true, nil
	})
}
func (s TenantStore) APIDeleteRole(ctx context.Context, id ID, expected int64) error {
	return s.roleWrite(ctx, expected, func(tx *sql.Tx) (ID, string, bool, error) {
		role, err := s.role(ctx, tx, id)
		if err != nil {
			return id, "", false, err
		}
		if role.BuiltIn {
			return id, "", false, ErrReservedRole
		}
		if role.BindingCount > 0 {
			return id, "", false, ErrRoleInUse
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM role_policies WHERE tenant_id=? AND role_id=?`, s.tenant, id); err != nil {
			return id, "", false, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM roles WHERE tenant_id=? AND id=?`, s.tenant, id); err != nil {
			return id, "", false, err
		}
		return id, "role.deleted", true, nil
	})
}
func (s TenantStore) APIMemberRoles(ctx context.Context, member ID) (items []RoleSummary, version int64, err error) {
	items = []RoleSummary{}
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, av, ov int64) error {
		version = av
		var count int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships WHERE tenant_id=? AND id=?`, s.tenant, member).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return ErrNotFound
		}
		rows, e := tx.QueryContext(ctx, `SELECT role_id FROM role_bindings WHERE tenant_id=? AND membership_id=? ORDER BY role_id`, s.tenant, member)
		if e != nil {
			return e
		}
		ids := []ID{}
		for rows.Next() {
			var id ID
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, id := range ids {
			r, e := s.role(ctx, tx, id)
			if e != nil {
				return e
			}
			items = append(items, r)
		}
		return nil
	})
	return
}
func (s TenantStore) APIBindRole(ctx context.Context, member, roleID ID, expected int64, remove bool) error {
	return s.roleWrite(ctx, expected, func(tx *sql.Tx) (ID, string, bool, error) {
		role, err := s.role(ctx, tx, roleID)
		if err != nil {
			return member, "", false, err
		}
		var state, userState string
		err = tx.QueryRowContext(ctx, `SELECT m.status,u.status FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.id=? FOR UPDATE`, s.tenant, member).Scan(&state, &userState)
		if err == sql.ErrNoRows {
			return member, "", false, ErrNotFound
		}
		if err != nil {
			return member, "", false, err
		}
		if !remove && (state != "active" || userState != "active") {
			return member, "", false, ErrDenied
		}
		var bound int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM role_bindings WHERE tenant_id=? AND membership_id=? AND role_id=?`, s.tenant, member, roleID).Scan(&bound); err != nil {
			return member, "", false, err
		}
		if (!remove && bound == 1) || (remove && bound == 0) {
			return member, "", false, nil
		}
		if remove && role.BuiltIn && state == "active" && userState == "active" {
			var count int
			err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships m JOIN users u ON u.id=m.user_id JOIN role_bindings b ON b.tenant_id=m.tenant_id AND b.membership_id=m.id WHERE b.tenant_id=? AND b.role_id=? AND m.status='active' AND u.status='active'`, s.tenant, roleID).Scan(&count)
			if err != nil {
				return member, "", false, err
			}
			if count <= 1 {
				return member, "", false, ErrLastOwner
			}
		}
		action := "role.bound"
		var event ID
		// Audit targets a binding event object, not a mutable policy document/proof.
		event, err = NewID()
		if err != nil {
			return member, "", false, err
		}
		if remove {
			_, err = tx.ExecContext(ctx, `DELETE FROM role_bindings WHERE tenant_id=? AND membership_id=? AND role_id=?`, s.tenant, member, roleID)
			action = "role.unbound"
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO role_bindings VALUES(?,?,?)`, s.tenant, member, roleID)
		}
		if err != nil {
			return event, "", false, err
		}
		operation := "bind"
		if remove {
			operation = "unbind"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO role_binding_changes(id,tenant_id,membership_id,role_id,role_code,operation) VALUES(?,?,?,?,?,?)`, event, s.tenant, member, roleID, role.Code, operation)
		return event, action, true, err
	})
}
