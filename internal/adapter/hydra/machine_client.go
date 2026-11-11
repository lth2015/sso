package hydra

import (
	"context"
)

// Machine profiles reuse the reviewed non-secret administrative projection.
// The secret is present only in the one create request, never in profile reads.
func MachineClientProfile(id, name, tenant, app, principal, credential, audience string) PublicClientProfile {
	return PublicClientProfile{ID: id, Name: name, Redirects: []string{}, Grants: []string{"client_credentials"}, Responses: []string{}, Scopes: "machine:invoke", Audience: []string{audience}, Authentication: "client_secret_basic", Metadata: map[string]string{"managed_by": "sso-platform", "kind": "machine-credential-v1", "tenant_id": tenant, "application_id": app, "service_principal_id": principal, "credential_id": credential}}
}
func (c *Client) CreateMachineClient(ctx context.Context, profile PublicClientProfile, secret string) error {
	// Unlike public registration, an existing machine client is never adopted:
	// its one-time secret cannot be recovered or independently proved here.
	_, found, err := c.ReadPublicClient(ctx, profile.ID)
	if err != nil {
		return err
	}
	if found {
		return ErrClientProfileConflict
	}
	body := struct {
		PublicClientProfile
		Secret string `json:"client_secret"`
	}{profile, secret}
	if err = c.Do(ctx, "POST", "/admin/clients", body, nil); err != nil {
		return err
	}
	actual, found, err := c.ReadPublicClient(ctx, profile.ID)
	if err != nil {
		return err
	}
	if !found {
		return ErrClientProfileConflict
	}
	if fields := profile.Differences(actual); len(fields) > 0 {
		return &ClientProfileConflict{Fields: fields}
	}
	return nil
}
