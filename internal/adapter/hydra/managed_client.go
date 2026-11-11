package hydra

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

var ErrClientProfileConflict = errors.New("protocol_client_conflict")

const ManagedClientScopes = "openid offline_access platform:read"
const ManagedClientAudience = "https://api.sso.local"

type PublicClientProfile struct {
	ID             string            `json:"client_id"`
	Name           string            `json:"client_name"`
	Redirects      []string          `json:"redirect_uris"`
	Grants         []string          `json:"grant_types"`
	Responses      []string          `json:"response_types"`
	Scopes         string            `json:"scope"`
	Audience       []string          `json:"audience"`
	Authentication string            `json:"token_endpoint_auth_method"`
	Metadata       map[string]string `json:"metadata"`
}

func ManagedPublicClient(id, name, redirect, tenant, app string) PublicClientProfile {
	return PublicClientProfile{id, name, []string{redirect}, []string{"authorization_code", "refresh_token"}, []string{"code"}, ManagedClientScopes, []string{ManagedClientAudience}, "none", map[string]string{"managed_by": "sso-platform", "kind": "public-pkce-v1", "tenant_id": tenant, "application_id": app, "registration_id": id}}
}

// Only decode reviewed non-secret fields. Neither upstream error bodies nor
// admin client_secret/registration tokens can be returned to a business caller.
func (c *Client) ReadPublicClient(ctx context.Context, id string) (PublicClientProfile, bool, error) {
	var v PublicClientProfile
	req, err := http.NewRequestWithContext(ctx, "GET", c.Base+"/admin/clients/"+url.PathEscape(id), nil)
	if err != nil {
		return v, false, errors.New("protocol request invalid")
	}
	r, err := c.HTTP.Do(req)
	if err != nil {
		return v, false, errors.New("protocol unavailable")
	}
	defer r.Body.Close()
	if r.StatusCode == 404 {
		return v, false, nil
	}
	if r.StatusCode != 200 {
		return v, false, errors.New("protocol request rejected")
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&v) != nil {
		return v, false, errors.New("protocol response invalid")
	}
	return v, true, nil
}
func sameSet(a, b []string) bool {
	a = slices.Clone(a)
	b = slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// ClientProfileConflict retains reviewed field names for private diagnostics;
// it never holds the upstream body or the differing values.
type ClientProfileConflict struct{ Fields []string }

func (e *ClientProfileConflict) Error() string { return "protocol_client_conflict" }
func (e *ClientProfileConflict) Unwrap() error { return ErrClientProfileConflict }
func (p PublicClientProfile) Differences(actual PublicClientProfile) []string {
	fields := []string{}
	for _, entry := range []struct {
		name  string
		equal bool
	}{
		{"client_id", p.ID == actual.ID}, {"client_name", p.Name == actual.Name}, {"token_endpoint_auth_method", p.Authentication == actual.Authentication},
		{"redirect_uris", sameSet(p.Redirects, actual.Redirects)}, {"grant_types", sameSet(p.Grants, actual.Grants)}, {"response_types", sameSet(p.Responses, actual.Responses)},
		{"scope", sameSet(strings.Fields(p.Scopes), strings.Fields(actual.Scopes))}, {"audience", sameSet(p.Audience, actual.Audience)},
	} {
		if !entry.equal {
			fields = append(fields, entry.name)
		}
	}
	metadata := len(p.Metadata) == len(actual.Metadata)
	for k, v := range p.Metadata {
		if actual.Metadata[k] != v {
			metadata = false
		}
	}
	if !metadata {
		fields = append(fields, "metadata")
	}
	return fields
}
func (p PublicClientProfile) Matches(actual PublicClientProfile) bool {
	return len(p.Differences(actual)) == 0
}

// EnsurePublicClient reads before any create; a lost POST reply is recovered by
// this exact profile/ownership comparison. Existing foreign objects are never
// overwritten or deleted. Request/response secrets are never retained or logged.
func (c *Client) EnsurePublicClient(ctx context.Context, profile PublicClientProfile) error {
	current, found, err := c.ReadPublicClient(ctx, profile.ID)
	if err != nil {
		return err
	}
	if !found {
		if err = c.Do(ctx, "POST", "/admin/clients", profile, nil); err != nil {
			return err
		}
		current, found, err = c.ReadPublicClient(ctx, profile.ID)
		if err != nil {
			return err
		}
	}
	if !found {
		return ErrClientProfileConflict
	}
	if fields := profile.Differences(current); len(fields) > 0 {
		return &ClientProfileConflict{Fields: fields}
	}
	return nil
}
func (c *Client) RemovePublicClient(ctx context.Context, profile PublicClientProfile) error {
	current, found, err := c.ReadPublicClient(ctx, profile.ID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if fields := profile.Differences(current); len(fields) > 0 {
		return &ClientProfileConflict{Fields: fields}
	}
	return c.Do(ctx, "DELETE", "/admin/clients/"+url.PathEscape(profile.ID), nil, nil)
}
