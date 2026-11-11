package mysql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func invitationRecipient(t *testing.T, s Store) (ID, string) {
	t.Helper()
	u := newUser(t, s)
	email := "reissue-" + u.String() + "@example.invalid"
	if _, err := s.db.Exec(`INSERT INTO login_aliases VALUES(?,?,UTC_TIMESTAMP(6))`, email, u); err != nil {
		t.Fatal("invitation recipient fixture failed")
	}
	t.Cleanup(func() {
		// Joined users are removed by the owning tenant fixture's later cleanup.
		if count(t, s, `SELECT COUNT(*) FROM memberships WHERE user_id=?`, u) == 0 {
			if _, err := s.db.Exec(`DELETE FROM login_aliases WHERE user_id=?`, u); err != nil {
				t.Error("unused invitation alias cleanup failed")
			}
			if _, err := s.db.Exec(`DELETE FROM users WHERE id=?`, u); err != nil {
				t.Error("unused invitation recipient cleanup failed")
			}
		}
	})
	return u, email
}

func TestInvitationReissueReplacesProofWithoutChangingResourceOrGrantingRoles(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	r := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	u, email := invitationRecipient(t, s)
	id, old, err := r.CreateInvitation(ctx, email)
	if err != nil {
		t.Fatal("invitation creation failed")
	}
	detail, err := r.APIInvitation(ctx, id)
	if err != nil || detail.Revision != 1 || detail.State != "pending" || detail.Email != email {
		t.Fatal("initial invitation metadata failed")
	}
	version := rbacVersion(t, s, b)
	issued, err := r.APIReissueInvitation(ctx, id, detail.Revision)
	if err != nil || len(issued.Token) != 43 || issued.Token == old || issued.ID != id.String() || issued.ExpiresIn != 86400 || issued.Revision != 2 {
		t.Fatal("invitation proof replacement failed")
	}
	if rbacVersion(t, s, b) != version {
		t.Fatal("proof replacement unnecessarily revoked tenant grants")
	}
	detail, err = r.APIInvitation(ctx, id)
	if err != nil || detail.Email != email || detail.ID != id.String() || detail.Revision != 2 || detail.Expires < time.Now().Add(24*time.Hour-time.Minute).Unix() || detail.Expires > time.Now().Add(24*time.Hour+time.Minute).Unix() {
		t.Fatal("replacement altered the target or expiry bounds")
	}
	metadata, _ := json.Marshal(detail)
	if bytes.Contains(metadata, []byte(old)) || bytes.Contains(metadata, []byte(issued.Token)) || bytes.Contains(metadata, []byte("digest")) {
		t.Fatal("private proof entered invitation metadata")
	}
	if _, err = s.AcceptInvitation(ctx, u, old); !errors.Is(err, ErrInvalid) {
		t.Fatal("old invitation proof survived replacement")
	}
	wrong, _ := invitationRecipient(t, s)
	if _, err = s.AcceptInvitation(ctx, wrong, issued.Token); !errors.Is(err, ErrDenied) {
		t.Fatal("replacement lost verified-email binding")
	}
	member, err := s.AcceptInvitation(ctx, u, issued.Token)
	if err != nil || count(t, s, `SELECT COUNT(*) FROM role_bindings WHERE tenant_id=? AND membership_id=?`, b.Tenant, member) != 0 {
		t.Fatal("replacement failed membership-only acceptance")
	}
	detail, err = r.APIInvitation(ctx, id)
	if err != nil || detail.State != "closed" || detail.Revision != 3 {
		t.Fatal("acceptance did not invalidate the invitation revision")
	}
	if result, err := r.APIReissueInvitation(ctx, id, 3); !errors.Is(err, ErrInvitationClosed) || result.Token != "" {
		t.Fatal("accepted invitation reopened or returned a proof")
	}
	if count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=? AND target_id=? AND action='invitation.reissued'`, b.Tenant, id) != 1 || count(t, s, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=? AND aggregate_id=? AND type='invitation.reissued' AND aggregate_version=2`, b.Tenant, id) != 1 {
		t.Fatal("replacement audit/outbox did not match its resource revision")
	}
}

func TestInvitationReissueExpiredClosedForeignAndNoopRevocation(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	r := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	id, _, err := r.CreateInvitation(ctx, "expired-reissue@example.invalid")
	if err != nil {
		t.Fatal("invitation creation failed")
	}
	if _, err = s.db.Exec(`UPDATE invitations SET expires_at=? WHERE id=?`, time.Now().Unix()-1, id); err != nil {
		t.Fatal("expired invitation fixture failed")
	}
	detail, err := r.APIInvitation(ctx, id)
	if err != nil || detail.State != "expired" || detail.Revision != 1 {
		t.Fatal("expired metadata failed")
	}
	foreign := fixture(t, s)
	other := s.ForTenant(foreign.Tenant, foreign.Membership)
	if _, err = other.APIInvitation(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign tenant read invitation detail")
	}
	if result, err := other.APIReissueInvitation(ctx, id, 1); !errors.Is(err, ErrNotFound) || result.Token != "" {
		t.Fatal("foreign tenant replaced invitation proof")
	}
	if _, err = r.APIReissueInvitation(ctx, id, 1); err != nil {
		t.Fatal("expired invitation could not be explicitly updated")
	}
	if err = r.APIRevokeInvitation(ctx, id); err != nil {
		t.Fatal("reissued invitation revocation failed")
	}
	if err = r.APIRevokeInvitation(ctx, id); err != nil {
		t.Fatal("repeated revocation failed")
	}
	detail, err = r.APIInvitation(ctx, id)
	if err != nil || detail.State != "closed" || detail.Revision != 3 || count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=? AND target_id=? AND action='invitation.revoked'`, b.Tenant, id) != 1 {
		t.Fatal("revocation version/noop semantics failed")
	}
	if result, err := r.APIReissueInvitation(ctx, id, 2); !errors.Is(err, ErrConflict) || result.Token != "" {
		t.Fatal("stale reissue bypassed resource revision")
	}
	if result, err := r.APIReissueInvitation(ctx, id, 3); !errors.Is(err, ErrInvitationClosed) || result.Token != "" {
		t.Fatal("revoked invitation reopened")
	}
}

func TestInvitationConcurrentReissueHasExactlyOneProofAndAudit(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	r := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	id, old, err := r.CreateInvitation(ctx, "concurrent-reissue@example.invalid")
	if err != nil {
		t.Fatal("invitation creation failed")
	}
	var results [2]InvitationReissued
	var failures [2]error
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results[i], failures[i] = r.APIReissueInvitation(ctx, id, 1) }()
	}
	close(start)
	wg.Wait()
	wins, conflicts := 0, 0
	for i, err := range failures {
		if err == nil {
			wins++
			digest := sha256.Sum256([]byte(results[i].Token))
			var stored []byte
			if e := s.db.QueryRow(`SELECT token_digest FROM invitations WHERE id=?`, id).Scan(&stored); e != nil || !bytes.Equal(stored, digest[:]) || results[i].Token == old {
				t.Fatal("winner proof did not match stored digest")
			}
		} else if errors.Is(err, ErrConflict) && results[i].Token == "" {
			conflicts++
		}
	}
	if wins != 1 || conflicts != 1 || count(t, s, `SELECT revision FROM invitation_revisions WHERE invitation_id=?`, id) != 2 || count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=? AND action='invitation.reissued'`, b.Tenant) != 1 {
		t.Fatal("concurrent update created multiple proofs or audits")
	}
}

func TestInvitationReissueUsesCurrentManagerAndCannotBypassExplicitDeny(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	r := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	creator := rbacMember(t, s, b)
	allow := rbacRole(t, s, b, "reissue_operator", []RolePolicy{{"invitations.manage", "allow"}})
	rbacBind(t, s, b, creator, allow, false)
	u, email := invitationRecipient(t, s)
	id, old, err := s.ForTenant(b.Tenant, creator).CreateInvitation(ctx, email)
	if err != nil {
		t.Fatal("delegated creation failed")
	}
	rbacBind(t, s, b, creator, allow, true)
	if _, err = s.AcceptInvitation(ctx, u, old); !errors.Is(err, ErrDenied) {
		t.Fatal("old creator authority was not rechecked")
	}
	if result, err := s.ForTenant(b.Tenant, creator).APIReissueInvitation(ctx, id, 1); !errors.Is(err, ErrDenied) || result.Token != "" {
		t.Fatal("former manager updated an invitation")
	}
	deny := rbacRole(t, s, b, "block_reissue", []RolePolicy{{"invitations.manage", "deny"}})
	rbacBind(t, s, b, b.Membership, deny, false)
	if result, err := r.APIReissueInvitation(ctx, id, 1); !errors.Is(err, ErrDenied) || result.Token != "" {
		t.Fatal("owner bypassed explicit invitation deny")
	}
	if count(t, s, `SELECT COUNT(*) FROM invitation_revisions WHERE invitation_id=?`, id) != 0 {
		t.Fatal("denied update changed the resource revision")
	}
	rbacBind(t, s, b, b.Membership, deny, true)
	issued, err := r.APIReissueInvitation(ctx, id, 1)
	if err != nil {
		t.Fatal("current manager could not update an abandoned invitation")
	}
	var responsible ID
	if err = s.db.QueryRow(`SELECT creator_membership_id FROM invitations WHERE id=?`, id).Scan(&responsible); err != nil || responsible != b.Membership {
		t.Fatal("replacement retained the former manager's authority")
	}
	if _, err = s.AcceptInvitation(ctx, u, issued.Token); err != nil {
		t.Fatal("current-manager replacement could not be accepted")
	}
}

func TestInvitationReissueRollbackNeverReturnsOrPersistsAnUnknownProof(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	r := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	id, old, err := r.CreateInvitation(ctx, "rollback-reissue@example.invalid")
	if err != nil {
		t.Fatal("invitation creation failed")
	}
	if _, err = s.db.Exec(`INSERT INTO invitation_revisions(invitation_id,revision) VALUES(?,?)`, id, int64(math.MaxInt64)); err != nil {
		t.Fatal("rollback fixture failed")
	}
	if result, err := r.APIReissueInvitation(ctx, id, math.MaxInt64); err == nil || result.Token != "" {
		t.Fatal("failed transaction returned a private proof")
	}
	digest := sha256.Sum256([]byte(old))
	var stored []byte
	if err = s.db.QueryRow(`SELECT token_digest FROM invitations WHERE id=?`, id).Scan(&stored); err != nil || !bytes.Equal(stored, digest[:]) {
		t.Fatal("failed revision update persisted the replacement digest")
	}
	if count(t, s, `SELECT COUNT(*) FROM audit_events WHERE tenant_id=? AND action='invitation.reissued'`, b.Tenant) != 0 || count(t, s, `SELECT COUNT(*) FROM outbox_events WHERE tenant_id=? AND type='invitation.reissued'`, b.Tenant) != 0 {
		t.Fatal("failed replacement left audit/outbox mutations")
	}
}

func TestInvitationAcceptanceAndReissueSerializeWithoutReopeningOrLeakingProof(t *testing.T) {
	s := testStore(t)
	b := fixture(t, s)
	r := s.ForTenant(b.Tenant, b.Membership)
	ctx := context.Background()
	for range 10 {
		u, email := invitationRecipient(t, s)
		id, old, err := r.CreateInvitation(ctx, email)
		if err != nil {
			t.Fatal("invitation creation failed")
		}
		var issued InvitationReissued
		var member ID
		var accepted, replaced error
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; member, accepted = s.AcceptInvitation(ctx, u, old) }()
		go func() { defer wg.Done(); <-start; issued, replaced = r.APIReissueInvitation(ctx, id, 1) }()
		close(start)
		wg.Wait()
		if replaced == nil {
			if !errors.Is(accepted, ErrInvalid) || !member.empty() || issued.Revision != 2 {
				t.Fatal("replaced proof accepted during the race")
			}
			if _, err = s.AcceptInvitation(ctx, u, issued.Token); err != nil {
				t.Fatal("winning replacement was unusable")
			}
		} else if !errors.Is(replaced, ErrConflict) || issued.Token != "" || accepted != nil || member.empty() {
			t.Fatal("acceptance/reissue did not serialize to one transition")
		}
		if count(t, s, `SELECT COUNT(*) FROM memberships WHERE tenant_id=? AND user_id=?`, b.Tenant, u) != 1 {
			t.Fatal("race created duplicate or missing memberships")
		}
		detail, err := r.APIInvitation(ctx, id)
		if err != nil || detail.State != "closed" || strings.Contains(issued.Token, old) {
			t.Fatal("race reopened a closed invitation or retained a proof")
		}
	}
}
