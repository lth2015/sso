package platformapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/coreos/go-oidc/v3/oidc"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

var machineResourcePath = regexp.MustCompile(`^/v1/machine/applications/([a-f0-9]{32})/identity$`)

// The resource route chooses a configured application namespace, never an
// arbitrary issuer/JWKS destination or a caller-supplied tenant header.
func (a *Authenticator) AuthenticateMachine(ctx context.Context, raw string, application store.ID) (result store.MachineAuthorization, err error) {
	if len(raw) > 16384 {
		return result, store.ErrDenied
	}
	audience := store.MachineAudience(application)
	token, e := a.Provider.Verifier(&oidc.Config{ClientID: audience, SupportedSigningAlgs: []string{"RS256"}}).Verify(oidc.ClientContext(ctx, a.HTTP), raw)
	if e != nil {
		return result, store.ErrDenied
	}
	var c struct {
		Subject  string   `json:"sub"`
		Client   string   `json:"client_id"`
		Audience []string `json:"aud"`
		Scopes   []string `json:"scp"`
		Ext      struct {
			Kind               string `json:"principal_type"`
			Tenant             string `json:"tenant_id"`
			Application        string `json:"application_id"`
			Principal          string `json:"principal_id"`
			Credential         string `json:"credential_id"`
			TenantVersion      string `json:"tenant_authz_version"`
			ApplicationVersion string `json:"application_version"`
			EnabledVersion     string `json:"enabled_version"`
			PrincipalVersion   string `json:"principal_version"`
			CredentialVersion  string `json:"credential_version"`
			CredentialExpires  string `json:"credential_expires_at"`
		} `json:"ext"`
	}
	if token.Claims(&c) != nil || c.Ext.Kind != "service_principal" || c.Ext.Application != application.String() || len(c.Audience) != 1 || c.Audience[0] != audience || c.Subject != c.Client || len(c.Scopes) != 1 || c.Scopes[0] != store.MachineScope {
		return result, store.ErrDenied
	}
	result.Application = application
	result.Tenant, e = store.ParseID(c.Ext.Tenant)
	if e != nil {
		return result, store.ErrDenied
	}
	result.Principal, e = store.ParseID(c.Ext.Principal)
	if e != nil {
		return result, store.ErrDenied
	}
	result.Credential, e = store.ParseID(c.Ext.Credential)
	if e != nil {
		return result, store.ErrDenied
	}
	result.Client = c.Client
	result.Expires = token.Expiry.Unix()
	// Hydra round-trips extension numbers through floating-point JSON. Use
	// canonical decimal strings to retain exact primary versions and timestamps.
	for _, field := range []struct {
		raw    string
		target *uint64
	}{
		{c.Ext.TenantVersion, &result.TenantVersion},
		{c.Ext.ApplicationVersion, &result.ApplicationVersion},
		{c.Ext.EnabledVersion, &result.EnabledVersion},
		{c.Ext.PrincipalVersion, &result.PrincipalVersion},
		{c.Ext.CredentialVersion, &result.CredentialVersion},
	} {
		v, err := strconv.ParseUint(field.raw, 10, 64)
		if err != nil || v == 0 || strconv.FormatUint(v, 10) != field.raw {
			return result, store.ErrDenied
		}
		*field.target = v
	}
	result.CredentialExpires, e = strconv.ParseInt(c.Ext.CredentialExpires, 10, 64)
	if e != nil || result.CredentialExpires <= 0 || strconv.FormatInt(result.CredentialExpires, 10) != c.Ext.CredentialExpires {
		return result, store.ErrDenied
	}
	return result, a.Store.CheckMachineResource(ctx, result)
}
func (a *Authenticator) machineResource(w http.ResponseWriter, r *http.Request, application store.ID) {
	if r.Method != "GET" || r.URL.RawQuery != "" {
		problem(w, 400, "invalid_request")
		return
	}
	raw, e := bearer(r)
	if e != nil {
		machineUnauthorized(w)
		return
	}
	claims, e := a.AuthenticateMachine(r.Context(), raw, application)
	if e != nil {
		if errors.Is(e, store.ErrDenied) {
			machineUnauthorized(w)
		} else {
			failure(w, e)
		}
		return
	}
	if hint := r.Header.Get("X-Tenant-ID"); hint != "" && hint != claims.Tenant.String() {
		problem(w, 403, "tenant_mismatch")
		return
	}
	respond(w, 200, map[string]any{"principal_type": "service_principal", "tenant_id": claims.Tenant.String(), "application_id": claims.Application.String(), "service_principal_id": claims.Principal.String(), "credential_id": claims.Credential.String(), "scope": store.MachineScope, "audience": store.MachineAudience(claims.Application)})
}

func machineUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="machine-resource", error="invalid_token"`)
	problem(w, 401, "invalid_token")
}
