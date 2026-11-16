package platformapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	store "github.com/lth2015/sso/internal/storage/mysql"
)

const bffAuthorityPath = "/internal/bff/authorization"
const bffCredentialPath = "/internal/bff/revocation-credentials"
const bffRevokePath = "/internal/bff/revoke"

func (a *Authenticator) bffClientID() string {
	if a.BFFClientID != "" {
		return a.BFFClientID
	}
	return "sso-bff"
}
func (a *Authenticator) bffInternal(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		problem(w, 400, "invalid_request")
		return
	}
	if r.URL.Path == bffRevokePath {
		if r.Method != "POST" {
			problem(w, 405, "method_not_allowed")
			return
		}
		headers := r.Header.Values("Authorization")
		if len(headers) != 1 {
			unauthorized(w)
			return
		}
		parts := strings.Fields(headers[0])
		if len(parts) != 2 || parts[0] != "BFF-Revoke" {
			unauthorized(w)
			return
		}
		raw, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil || len(raw) != 32 {
			unauthorized(w)
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		if !decodeBFFBody(w, r, &body) {
			return
		}
		digest := sha256.Sum256(raw)
		err = a.Store.APIRevokeBFFGrant(r.Context(), digest[:], a.bffClientID(), body.Reason)
		if errors.Is(err, store.ErrBFFRevocationExpired) {
			problem(w, 410, "revocation_credential_expired")
			return
		}
		if errors.Is(err, store.ErrDenied) {
			unauthorized(w)
			return
		}
		if err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	if (r.URL.Path == bffAuthorityPath && r.Method != "GET") || (r.URL.Path == bffCredentialPath && r.Method != "POST") {
		problem(w, 405, "method_not_allowed")
		return
	}
	raw, err := bearer(r)
	if err != nil {
		unauthorized(w)
		return
	}
	p, err := a.Authenticate(r.Context(), raw)
	if errors.Is(err, store.ErrDenied) {
		unauthorized(w)
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	if p.Authority.Client != a.bffClientID() || !p.Scopes["platform:read"] {
		problem(w, 403, "insufficient_scope")
		return
	}
	if r.URL.Path == bffAuthorityPath {
		v := p.Authority
		respond(w, 200, map[string]string{"user": v.User.String(), "tenant": v.Tenant.String(), "grant": v.Grant.String(), "member": v.Member.String(), "session": v.Session.String(), "client": v.Client})
		return
	}
	var body struct{}
	if !decodeBFFBody(w, r, &body) {
		return
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		problem(w, 503, "temporarily_unavailable")
		return
	}
	digest := sha256.Sum256(secret)
	expiry, err := a.Store.APIIssueBFFRevocation(r.Context(), p.Authority, digest[:])
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, 201, map[string]any{"credential": base64.RawURLEncoding.EncodeToString(secret), "expires_at": expiry})
}
func decodeBFFBody(w http.ResponseWriter, r *http.Request, target any) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		problem(w, 415, "unsupported_media_type")
		return false
	}
	data, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 512))
	if readErr != nil || !strictObject(data) {
		problem(w, 400, "invalid_request")
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		problem(w, 400, "invalid_request")
		return false
	}

	return true
}

// Readiness checks all storage needed by the private BFF authority API.
func bffAuthorityReady(ctx context.Context, repo store.Store) error {
	return repo.BFFAuthorityReady(ctx)
}
