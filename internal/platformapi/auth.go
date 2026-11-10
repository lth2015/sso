package platformapi

import (
	"context"
	"database/sql"
	"errors"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/lth2015/sso/internal/adapter/hydra"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http"
	"strings"
	"time"
)

const Audience = "https://api.sso.local"
const Origin = "http://api.localhost:8083"

// ProtocolTransport has one preconfigured destination; JWT headers cannot select a JWKS URL.
type ProtocolTransport struct{ Base http.RoundTripper }

func (t ProtocolTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "http" || r.URL.Host != "auth.localhost:4464" {
		return nil, errors.New("untrusted issuer destination")
	}
	c := r.Clone(r.Context())
	u := *r.URL
	c.URL = &u
	c.URL.Host = "hydra-login:4444"
	c.Host = ""
	return t.Base.RoundTrip(c)
}

type Authenticator struct {
	Provider *oidc.Provider
	HTTP     *http.Client
	Store    store.Store
}

func NewAuthenticator(ctx context.Context, repo store.Store) (*Authenticator, error) {
	client := &http.Client{Transport: ProtocolTransport{&http.Transport{Proxy: nil}}, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), hydra.Issuer)
	if err != nil {
		return nil, errors.New("issuer unavailable")
	}
	return &Authenticator{provider, client, repo}, nil
}

type Principal struct {
	Authority store.Authorization
	Scopes    map[string]bool
}

func (a *Authenticator) Authenticate(ctx context.Context, raw string) (Principal, error) {
	var p Principal
	if len(raw) > 16384 {
		return p, store.ErrDenied
	}
	token, err := a.Provider.Verifier(&oidc.Config{ClientID: Audience, SupportedSigningAlgs: []string{"RS256"}}).Verify(oidc.ClientContext(ctx, a.HTTP), raw)
	if err != nil {
		return p, store.ErrDenied
	}
	var c struct {
		Subject  string   `json:"sub"`
		Audience []string `json:"aud"`
		Client   string   `json:"client_id"`
		Scopes   []string `json:"scp"`
		Ext      struct {
			Type    string `json:"principal_type"`
			User    string `json:"principal_id"`
			Tenant  string `json:"tenant_id"`
			Grant   string `json:"business_context_id"`
			Member  string `json:"membership_id"`
			Session string `json:"session_id"`
		} `json:"ext"`
	}
	if token.Claims(&c) != nil || len(c.Audience) != 1 || c.Audience[0] != Audience || c.Ext.Type != "user" || c.Subject != c.Ext.User || c.Client == "" {
		return p, store.ErrDenied
	}
	p.Authority.Grant, err = store.ParseID(c.Ext.Grant)
	if err != nil {
		return p, store.ErrDenied
	}
	p.Authority.User, err = store.ParseID(c.Subject)
	if err != nil {
		return p, store.ErrDenied
	}
	p.Authority.Tenant, err = store.ParseID(c.Ext.Tenant)
	if err != nil {
		return p, store.ErrDenied
	}
	p.Authority.Member, err = store.ParseID(c.Ext.Member)
	if err != nil {
		return p, store.ErrDenied
	}
	p.Authority.Session, err = store.ParseID(c.Ext.Session)
	if err != nil {
		return p, store.ErrDenied
	}
	p.Authority.Expires = token.Expiry.Unix()
	p.Authority.Client = c.Client
	p.Scopes = map[string]bool{}
	for _, scope := range c.Scopes {
		p.Scopes[scope] = true
	}
	if !p.Scopes["platform:read"] && !p.Scopes["platform:write"] {
		return p, store.ErrDenied
	}
	return p, a.Store.APITransaction(ctx, p.Authority, func(_ *sql.Tx) error { return nil })
}
func bearer(r *http.Request) (string, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", store.ErrDenied
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", store.ErrDenied
	}
	return parts[1], nil
}
