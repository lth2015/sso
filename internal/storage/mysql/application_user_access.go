package mysql

import (
	"context"
	"database/sql"
	"math"
	"strconv"
)

const (
	UserAccessAll      = "all_members"
	UserAccessSelected = "selected_members"
)

// All aliases refer to the same currently checked tenant/member/application.
// These predicates are also used at consent and at every User grant boundary.
const userAccessJoinSQL = ` LEFT JOIN application_user_access_policies uap ON uap.tenant_id=ta.tenant_id AND uap.application_id=ta.application_id `
const userAccessAllowedSQL = `(uap.application_id IS NULL OR uap.mode='all_members' OR (uap.mode='selected_members' AND (
 EXISTS(SELECT 1 FROM role_bindings uab JOIN roles uar ON uar.tenant_id=uab.tenant_id AND uar.id=uab.role_id WHERE uab.tenant_id=m.tenant_id AND uab.membership_id=m.id AND uar.code='owner')
 OR EXISTS(SELECT 1 FROM application_user_access_members uam WHERE uam.tenant_id=m.tenant_id AND uam.application_id=ta.application_id AND uam.membership_id=m.id))))`
const userAccessGrantJoinSQL = userAccessJoinSQL + ` LEFT JOIN application_user_grant_versions uag ON uag.grant_id=g.id `
const userAccessGrantAllowedSQL = userAccessAllowedSQL + ` AND COALESCE(uag.version,1)=COALESCE(uap.version,1) AND (uag.grant_id IS NULL OR (uag.tenant_id=g.tenant_id AND uag.application_id=ta.application_id)) `

type ApplicationUserAccessPolicy struct {
	Tenant      string `json:"tenant_id"`
	Application string `json:"application_id"`
	Mode        string `json:"mode"`
	Version     string `json:"version"`
	revision    int64
}
type ApplicationUserAccessMember struct {
	Member string `json:"membership_id"`
}
type ApplicationUserAccessPage struct {
	Policy ApplicationUserAccessPolicy   `json:"policy"`
	Items  []ApplicationUserAccessMember `json:"items"`
	Next   *string                       `json:"next_cursor"`
}
type ApplicationUserAccessDecision struct {
	Policy   ApplicationUserAccessPolicy `json:"policy"`
	Member   string                      `json:"membership_id"`
	Allowed  bool                        `json:"allowed"`
	Reason   string                      `json:"reason"`
	Explicit bool                        `json:"explicit_grant"`
}

func validUserAccessMode(mode string) bool {
	return mode == UserAccessAll || mode == UserAccessSelected
}
func (s TenantStore) userAccessPolicy(ctx context.Context, tx *sql.Tx, app ID) (p ApplicationUserAccessPolicy, err error) {
	p = ApplicationUserAccessPolicy{Tenant: s.tenant.String(), Application: app.String(), Mode: UserAccessAll, Version: "1", revision: 1}
	err = tx.QueryRowContext(ctx, `SELECT mode,version FROM application_user_access_policies WHERE tenant_id=? AND application_id=?`, s.tenant, app).Scan(&p.Mode, &p.revision)
	if err == sql.ErrNoRows {
		err = nil
	}
	if err != nil {
		return ApplicationUserAccessPolicy{}, err
	}
	if !validUserAccessMode(p.Mode) || p.revision < 1 {
		return ApplicationUserAccessPolicy{}, ErrInvalid
	}
	p.Version = strconv.FormatInt(p.revision, 10)
	return
}
func (s TenantStore) APIApplicationUserAccess(ctx context.Context, app ID, limit int, after ID) (page ApplicationUserAccessPage, err error) {
	if limit < 1 || limit > 100 {
		return page, ErrInvalid
	}
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if e := s.ownedApplication(ctx, tx, app, false); e != nil {
			return e
		}
		var e error
		page.Policy, e = s.userAccessPolicy(ctx, tx, app)
		if e != nil {
			return e
		}
		page.Items = []ApplicationUserAccessMember{}
		rows, e := tx.QueryContext(ctx, `SELECT LOWER(HEX(membership_id)) FROM application_user_access_members WHERE tenant_id=? AND application_id=? AND membership_id>? ORDER BY membership_id LIMIT ?`, s.tenant, app, after, limit+1)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v ApplicationUserAccessMember
			if e = rows.Scan(&v.Member); e != nil {
				return e
			}
			page.Items = append(page.Items, v)
		}
		if e = rows.Err(); e != nil {
			return e
		}
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			v := page.Items[len(page.Items)-1].Member
			page.Next = &v
		}
		return nil
	})
	if err != nil {
		return ApplicationUserAccessPage{}, err
	}
	return
}
func (s TenantStore) APIApplicationUserAccessDecision(ctx context.Context, app, member ID) (v ApplicationUserAccessDecision, err error) {
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if e := s.ownedApplication(ctx, tx, app, false); e != nil {
			return e
		}
		var e error
		v.Policy, e = s.userAccessPolicy(ctx, tx, app)
		if e != nil {
			return e
		}
		var membership, user string
		var owner, explicit, enabled bool
		e = tx.QueryRowContext(ctx, `SELECT m.status,u.status,
 EXISTS(SELECT 1 FROM role_bindings b JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id WHERE b.tenant_id=m.tenant_id AND b.membership_id=m.id AND r.code='owner'),
 EXISTS(SELECT 1 FROM application_user_access_members am WHERE am.tenant_id=m.tenant_id AND am.membership_id=m.id AND am.application_id=?),
 a.active AND ta.active
 FROM memberships m JOIN users u ON u.id=m.user_id JOIN tenant_applications ta ON ta.tenant_id=m.tenant_id AND ta.application_id=? JOIN applications a ON a.id=ta.application_id WHERE m.tenant_id=? AND m.id=?`, app, app, s.tenant, member).Scan(&membership, &user, &owner, &explicit, &enabled)
		if e == sql.ErrNoRows {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		v.Member = member.String()
		v.Explicit = explicit
		switch {
		case !enabled:
			v.Reason = "application_disabled"
		case user != "active":
			v.Reason = "user_inactive"
		case membership != "active":
			v.Reason = "membership_inactive"
		case owner:
			v.Allowed = true
			v.Reason = "owner"
		case v.Policy.Mode == UserAccessAll:
			v.Allowed = true
			v.Reason = "all_members"
		case explicit:
			v.Allowed = true
			v.Reason = "explicit_grant"
		default:
			v.Reason = "not_granted"
		}
		return nil
	})
	if err != nil {
		return ApplicationUserAccessDecision{}, err
	}
	return
}
func (s TenantStore) SetApplicationUserAccessMode(ctx context.Context, app ID, mode string, expected int64) (ApplicationUserAccessPolicy, error) {
	if !validUserAccessMode(mode) {
		return ApplicationUserAccessPolicy{}, ErrInvalid
	}
	return s.changeApplicationUserAccess(ctx, app, ID{}, &mode, false, expected)
}
func (s TenantStore) SetApplicationUserAccessMember(ctx context.Context, app, member ID, grant bool, expected int64) (ApplicationUserAccessPolicy, error) {
	if member.empty() {
		return ApplicationUserAccessPolicy{}, ErrInvalid
	}
	return s.changeApplicationUserAccess(ctx, app, member, nil, grant, expected)
}
func (s TenantStore) changeApplicationUserAccess(ctx context.Context, app, member ID, mode *string, grant bool, expected int64) (p ApplicationUserAccessPolicy, err error) {
	if expected < 1 {
		return p, ErrInvalid
	}
	s = s.withRecentMFA()
	err = s.publicRead(ctx, RoleManagement, func(tx *sql.Tx, _, _ int64) error {
		if e := s.ownedApplication(ctx, tx, app, false); e != nil {
			return e
		}
		var e error
		p, e = s.userAccessPolicy(ctx, tx, app)
		if e != nil {
			return e
		}
		if p.revision != expected {
			return ErrConflict
		}
		action := "application.user_access.mode_changed"
		if mode != nil {
			if p.Mode == *mode {
				return nil
			}
			p.Mode = *mode
		} else {
			var ms, us string
			e = tx.QueryRowContext(ctx, `SELECT m.status,u.status FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.tenant_id=? AND m.id=? FOR SHARE`, s.tenant, member).Scan(&ms, &us)
			if e == sql.ErrNoRows {
				return ErrNotFound
			}
			if e != nil {
				return e
			}
			if grant && (ms != "active" || us != "active") {
				return ErrInvalid
			}
			var present int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM application_user_access_members WHERE tenant_id=? AND application_id=? AND membership_id=?`, s.tenant, app, member).Scan(&present); e != nil {
				return e
			}
			if (present == 1) == grant {
				return nil
			}
			action = "application.user_access.revoked"
			if grant {
				action = "application.user_access.granted"
			}
		}
		if p.revision == math.MaxInt64 {
			return ErrInvalid
		}
		p.revision++
		p.Version = strconv.FormatInt(p.revision, 10)
		if _, e = tx.ExecContext(ctx, `INSERT INTO application_user_access_policies(tenant_id,application_id,mode,version) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE mode=VALUES(mode),version=VALUES(version)`, s.tenant, app, p.Mode, p.revision); e != nil {
			return e
		}
		if mode == nil {
			if grant {
				_, e = tx.ExecContext(ctx, `INSERT INTO application_user_access_members(tenant_id,application_id,membership_id) VALUES(?,?,?)`, s.tenant, app, member)
			} else {
				_, e = tx.ExecContext(ctx, `DELETE FROM application_user_access_members WHERE tenant_id=? AND application_id=? AND membership_id=?`, s.tenant, app, member)
			}
			if e != nil {
				return e
			}
		}
		return s.recordApplicationUserAccess(ctx, tx, app, member, p, action)
	})
	if err != nil {
		return ApplicationUserAccessPolicy{}, err
	}
	return
}
func (s TenantStore) recordApplicationUserAccess(ctx context.Context, tx *sql.Tx, app, member ID, p ApplicationUserAccessPolicy, action string) error {
	event, e := NewID()
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO audit_events(event_id,tenant_id,actor_membership_id,action,target_id) VALUES(?,?,?,?,?)`, event, s.tenant, s.actor, action, app); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO outbox_events(event_id,tenant_id,aggregate_id,aggregate_version,type) VALUES(?,?,?,?,?)`, event, s.tenant, app, p.revision, action); e != nil {
		return e
	}
	var target any
	if !member.empty() {
		target = member
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO application_user_access_changes(event_id,tenant_id,application_id,membership_id,mode,version) VALUES(?,?,?,?,?,?)`, event, s.tenant, app, target, p.Mode, p.revision)
	return e
}
