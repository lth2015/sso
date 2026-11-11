package mysql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lth2015/sso/internal/adapter/hydra"
)

var ErrObservationChanged = errors.New("observation_changed")

type ProtocolObservation struct {
	Kind               string    `json:"resource_kind"`
	Tenant             string    `json:"tenant_id"`
	Application        string    `json:"application_id"`
	Principal          *string   `json:"service_principal_id"`
	Resource           string    `json:"resource_id"`
	Revision           int64     `json:"revision"`
	ResourceETag       string    `json:"resource_etag"`
	LocalState         string    `json:"local_state"`
	ProfileState       string    `json:"profile_state"`
	Differences        []string  `json:"differences"`
	ObservedAt         time.Time `json:"observed_at"`
	AuthorityCheckedAt time.Time `json:"authority_checked_at"`
}

type protocolSnapshot struct {
	profile  hydra.PublicClientProfile
	revision int64
	state    string
}

// Each local read independently checks current primary authority. No database
// transaction/tenant lock spans the private Admin request. No mutations occur.
func (s TenantStore) observeProtocol(ctx context.Context, kind string, app, resource ID, principal *string, read func(context.Context) (protocolSnapshot, error), admin *hydra.Client) (ProtocolObservation, error) {
	if admin == nil {
		return ProtocolObservation{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	initial, err := read(ctx)
	if err != nil {
		return ProtocolObservation{}, err
	}
	actual, found, err := admin.ReadPublicClient(ctx, initial.profile.ID)
	if err != nil {
		return ProtocolObservation{}, err
	}
	observed := time.Now().UTC()
	current, err := read(ctx)
	if err != nil {
		return ProtocolObservation{}, err
	}
	if current.revision != initial.revision || !initial.profile.Matches(current.profile) {
		return ProtocolObservation{}, ErrObservationChanged
	}
	result := ProtocolObservation{Kind: kind, Tenant: s.tenant.String(), Application: app.String(), Principal: principal, Resource: resource.String(), Revision: current.revision, LocalState: current.state, ProfileState: "absent", Differences: []string{}, ObservedAt: observed, AuthorityCheckedAt: time.Now().UTC()}
	tagKind := "client"
	if kind == "machine_credential" {
		tagKind = "cred"
	}
	result.ResourceETag = fmt.Sprintf("\"%s-%s-%s-%d\"", tagKind, s.tenant.String(), resource.String(), current.revision)
	if found {
		result.Differences = initial.profile.Differences(actual)
		result.ProfileState = "matching"
		if len(result.Differences) > 0 {
			result.ProfileState = "conflict"
		}
	}
	return result, nil
}
func (s TenantStore) APIManagedClientProtocol(ctx context.Context, app, client ID, admin *hydra.Client) (ProtocolObservation, error) {
	read := func(ctx context.Context) (protocolSnapshot, error) {
		v, err := s.APIManagedClient(ctx, app, client)
		if err != nil {
			return protocolSnapshot{}, err
		}
		return protocolSnapshot{hydra.ManagedPublicClient(v.ID, v.Name, v.Redirect, s.tenant.String(), app.String()), v.Version, v.State}, nil
	}
	return s.observeProtocol(ctx, "public_client", app, client, nil, read, admin)
}
func (s TenantStore) APIMachineCredentialProtocol(ctx context.Context, app, principal, credential ID, admin *hydra.Client) (ProtocolObservation, error) {
	read := func(ctx context.Context) (protocolSnapshot, error) {
		v, err := s.APIMachineCredential(ctx, app, principal, credential)
		if err != nil {
			return protocolSnapshot{}, err
		}
		return protocolSnapshot{hydra.MachineClientProfile(v.AccessKey, v.Name, s.tenant.String(), app.String(), principal.String(), v.ID, v.Audience), v.Version, v.State}, nil
	}
	p := principal.String()
	return s.observeProtocol(ctx, "machine_credential", app, credential, &p, read, admin)
}
