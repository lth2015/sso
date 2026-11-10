package mysql

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
)

var (
	ErrDenied    = errors.New("denied")
	ErrNotFound  = errors.New("not_found")
	ErrConflict  = errors.New("version_conflict")
	ErrInvalid   = errors.New("invalid_input")
	ErrLastOwner = errors.New("last_active_owner")
	ErrCycle     = errors.New("organization_cycle")
	ErrRoot      = errors.New("root_cannot_move")
)
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) Store { return Store{db: db} }

type Bootstrap struct{ Tenant, Membership, Root ID }
type Org struct {
	ID     ID
	Parent *ID
	Name   string
}

// BootstrapTenant is internal: its caller must first verify global user identity
// and tenant-creation policy. It does not authenticate an arbitrary browser user.
func (s Store) BootstrapTenant(ctx context.Context, user ID, slug, name string) (Bootstrap, error) {
	if user.empty() || !slugPattern.MatchString(slug) || strings.TrimSpace(name) == "" || len(name) > 128 {
		return Bootstrap{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Bootstrap{}, err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM users WHERE id=? FOR SHARE`, user).Scan(&status); err != nil {
		return Bootstrap{}, err
	}
	if status != "active" {
		return Bootstrap{}, ErrDenied
	}
	b, err := bootstrapTenantTx(ctx, tx, user, slug, name)
	if err != nil {
		return Bootstrap{}, err
	}
	if err = tx.Commit(); err != nil {
		return Bootstrap{}, err
	}
	return b, nil
}

func bootstrapTenantTx(ctx context.Context, tx *sql.Tx, user ID, slug, name string) (Bootstrap, error) {
	var err error
	var b Bootstrap
	if b.Tenant, err = NewID(); err != nil {
		return b, err
	}
	if b.Membership, err = NewID(); err != nil {
		return b, err
	}
	if b.Root, err = NewID(); err != nil {
		return b, err
	}
	role, err := NewID()
	if err != nil {
		return b, err
	}
	statements := []struct {
		q string
		a []any
	}{
		{`INSERT INTO tenants(id,slug) VALUES(?,?)`, []any{b.Tenant, slug}},
		{`INSERT INTO memberships(id,tenant_id,user_id) VALUES(?,?,?)`, []any{b.Membership, b.Tenant, user}},
		{`INSERT INTO org_nodes(id,tenant_id,name) VALUES(?,?,?)`, []any{b.Root, b.Tenant, name}},
		{`INSERT INTO tenant_roots(tenant_id,org_id) VALUES(?,?)`, []any{b.Tenant, b.Root}},
		{`INSERT INTO org_closure VALUES(?,?,?,0)`, []any{b.Tenant, b.Root, b.Root}},
		{`INSERT INTO roles(id,tenant_id,code) VALUES(?,?,'owner')`, []any{role, b.Tenant}},
		{`INSERT INTO role_bindings VALUES(?,?,?)`, []any{b.Tenant, b.Membership, role}},
	}
	for _, st := range statements {
		if _, err = tx.ExecContext(ctx, st.q, st.a...); err != nil {
			return Bootstrap{}, err
		}
	}
	if err = record(ctx, tx, b.Tenant, b.Membership, "tenant.created", b.Tenant, 1); err != nil {
		return Bootstrap{}, err
	}
	return b, nil
}

// TenantStore has an explicit tenant and actor. It is an internal capability,
// never constructed from unverified HTTP headers or a browser-supplied actor.
type TenantStore struct {
	store         Store
	tenant, actor ID
	authority     *Authorization
}

func (s Store) ForTenant(tenant, actor ID) TenantStore {
	return TenantStore{store: s, tenant: tenant, actor: actor}
}

// lockOwner serializes safety-critical changes per tenant, then checks active
// User, Membership, Tenant and owner binding from the primary inside the tx.
func (s TenantStore) lockOwner(ctx context.Context, tx *sql.Tx) (int64, int64, error) {
	if s.tenant.empty() || s.actor.empty() {
		return 0, 0, ErrDenied
	}
	var authz, org int64
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status,authz_version,org_version FROM tenants WHERE id=? FOR UPDATE`, s.tenant).Scan(&status, &authz, &org)
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
	var count int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships m JOIN users u ON u.id=m.user_id JOIN role_bindings b ON b.tenant_id=m.tenant_id AND b.membership_id=m.id JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE m.tenant_id=? AND m.id=? AND m.status='active' AND u.status='active' AND r.code='owner'`, s.tenant, s.actor).Scan(&count)
	if err != nil {
		return 0, 0, err
	}
	if count != 1 {
		return 0, 0, ErrDenied
	}
	return authz, org, nil
}
func record(ctx context.Context, tx *sql.Tx, tenant, actor ID, action string, target ID, version int64) error {
	event, err := NewID()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(event_id,tenant_id,actor_membership_id,action,target_id) VALUES(?,?,?,?,?)`, event, tenant, actor, action, target); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(event_id,tenant_id,aggregate_id,aggregate_version,type) VALUES(?,?,?,?,?)`, event, tenant, target, version, action)
	return err
}
func (s TenantStore) ListOrganizations(ctx context.Context) ([]Org, error) {
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// This initial management slice is owner-only until scoped RBAC is implemented.
	if _, _, err = s.lockOwner(ctx, tx); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,parent_id,name FROM org_nodes WHERE tenant_id=? ORDER BY id`, s.tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Org{}
	for rows.Next() {
		var o Org
		var parent []byte
		if err = rows.Scan(&o.ID, &parent, &o.Name); err != nil {
			return nil, err
		}
		if parent != nil {
			var id ID
			if err = id.Scan(parent); err != nil {
				return nil, err
			}
			o.Parent = &id
		}
		result = append(result, o)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

func (s TenantStore) AddOrganization(ctx context.Context, parent ID, name string, expectedVersion int64) (ID, error) {
	if strings.TrimSpace(name) == "" || len(name) > 128 {
		return ID{}, ErrInvalid
	}
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ID{}, err
	}
	defer tx.Rollback()
	_, version, err := s.lockOwner(ctx, tx)
	if err != nil {
		return ID{}, err
	}
	if version != expectedVersion {
		return ID{}, ErrConflict
	}
	var depth, count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX(depth),0) FROM org_closure WHERE tenant_id=? AND descendant_id=?`, s.tenant, parent).Scan(&count, &depth); err != nil {
		return ID{}, err
	}
	if count == 0 {
		return ID{}, ErrNotFound
	}
	if depth >= 16 {
		return ID{}, ErrInvalid
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM org_nodes WHERE tenant_id=?`, s.tenant).Scan(&count); err != nil {
		return ID{}, err
	}
	if count >= 10000 {
		return ID{}, ErrInvalid
	}
	id, err := NewID()
	if err != nil {
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO org_nodes(id,tenant_id,parent_id,name) VALUES(?,?,?,?)`, id, s.tenant, parent, name); err != nil {
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO org_closure(tenant_id,ancestor_id,descendant_id,depth) SELECT tenant_id,ancestor_id,?,depth+1 FROM org_closure WHERE tenant_id=? AND descendant_id=?`, id, s.tenant, parent); err != nil {
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO org_closure VALUES(?,?,?,0)`, s.tenant, id, id); err != nil {
		return ID{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenants SET org_version=org_version+1 WHERE id=?`, s.tenant); err != nil {
		return ID{}, err
	}
	if err = record(ctx, tx, s.tenant, s.actor, "org.created", id, version+1); err != nil {
		return ID{}, err
	}
	return id, tx.Commit()
}

// SuspendMember preserves at least one active owner with an active User. The
// tenant row lock prevents two concurrent removals from both passing the count.
func (s TenantStore) SuspendMember(ctx context.Context, target ID, expectedVersion int64) error {
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	version, _, err := s.lockOwner(ctx, tx)
	if err != nil {
		return err
	}
	if version != expectedVersion {
		return ErrConflict
	}
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM memberships WHERE tenant_id=? AND id=? FOR UPDATE`, s.tenant, target).Scan(&status); err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "active" {
		return ErrConflict
	}
	var isOwner, ownerCount int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM role_bindings b JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE b.tenant_id=? AND b.membership_id=? AND r.code='owner'`, s.tenant, target).Scan(&isOwner); err != nil {
		return err
	}
	if isOwner > 0 {
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships m JOIN users u ON u.id=m.user_id JOIN role_bindings b ON b.tenant_id=m.tenant_id AND b.membership_id=m.id JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE m.tenant_id=? AND m.status='active' AND u.status='active' AND r.code='owner'`, s.tenant).Scan(&ownerCount)
		if err != nil {
			return err
		}
		if ownerCount <= 1 {
			return ErrLastOwner
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE memberships SET status='suspended',security_version=security_version+1 WHERE tenant_id=? AND id=?`, s.tenant, target); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenants SET authz_version=authz_version+1 WHERE id=?`, s.tenant); err != nil {
		return err
	}
	if err = record(ctx, tx, s.tenant, s.actor, "member.suspended", target, version+1); err != nil {
		return err
	}
	return tx.Commit()
}

type MovePlan struct {
	Nodes, Depth int
	Version      int64
}

// MoveOrganization supports a read-only preview. Its future HTTP caller must
// require recent MFA before executing; this repository is never public directly.
func (s TenantStore) MoveOrganization(ctx context.Context, node, parent ID, expectedVersion int64, dryRun bool) (MovePlan, error) {
	var plan MovePlan
	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return plan, err
	}
	defer tx.Rollback()
	_, version, err := s.lockOwner(ctx, tx)
	if err != nil {
		return plan, err
	}
	plan.Version = version
	if version != expectedVersion {
		return plan, ErrConflict
	}
	var current []byte
	err = tx.QueryRowContext(ctx, `SELECT parent_id FROM org_nodes WHERE tenant_id=? AND id=?`, s.tenant, node).Scan(&current)
	if err == sql.ErrNoRows {
		return plan, ErrNotFound
	}
	if err != nil {
		return plan, err
	}
	if current == nil {
		return plan, ErrRoot
	}
	var cycles, parentCount, parentDepth, height int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM org_closure WHERE tenant_id=? AND ancestor_id=? AND descendant_id=?`, s.tenant, node, parent).Scan(&cycles); err != nil {
		return plan, err
	}
	if cycles > 0 {
		return plan, ErrCycle
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX(depth),0) FROM org_closure WHERE tenant_id=? AND descendant_id=?`, s.tenant, parent).Scan(&parentCount, &parentDepth); err != nil {
		return plan, err
	}
	if parentCount == 0 {
		return plan, ErrNotFound
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),MAX(depth) FROM org_closure WHERE tenant_id=? AND ancestor_id=?`, s.tenant, node).Scan(&plan.Nodes, &height); err != nil {
		return plan, err
	}
	plan.Depth = parentDepth + 1 + height
	if plan.Depth > 16 {
		return plan, ErrInvalid
	}
	if dryRun {
		return plan, nil
	}
	if _, err = tx.ExecContext(ctx, `DELETE old FROM org_closure old JOIN org_closure sub ON sub.tenant_id=old.tenant_id AND sub.ancestor_id=? AND sub.descendant_id=old.descendant_id JOIN org_closure anc ON anc.tenant_id=old.tenant_id AND anc.descendant_id=? AND anc.ancestor_id=old.ancestor_id WHERE old.tenant_id=? AND anc.ancestor_id<>?`, node, node, s.tenant, node); err != nil {
		return plan, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO org_closure(tenant_id,ancestor_id,descendant_id,depth) SELECT super.tenant_id,super.ancestor_id,sub.descendant_id,super.depth+sub.depth+1 FROM org_closure super JOIN org_closure sub ON sub.tenant_id=super.tenant_id WHERE super.tenant_id=? AND super.descendant_id=? AND sub.ancestor_id=?`, s.tenant, parent, node); err != nil {
		return plan, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE org_nodes SET parent_id=? WHERE tenant_id=? AND id=?`, parent, s.tenant, node); err != nil {
		return plan, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenants SET org_version=org_version+1,authz_version=authz_version+1 WHERE id=?`, s.tenant); err != nil {
		return plan, err
	}
	if err = record(ctx, tx, s.tenant, s.actor, "org.moved", node, version+1); err != nil {
		return plan, err
	}
	return plan, tx.Commit()
}
