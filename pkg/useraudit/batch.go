package useraudit

import (
	"bytes"
	"encoding/json"
	"io"
	"time"
)

// Batch is the frozen sso.audit.batch.v1 projection, not a signed archive or a
// cross-page snapshot. Each call is independently authorized by the SSO primary.
type Batch[T any] struct {
	Format       string    `json:"format"`
	Kind         string    `json:"kind"`
	TenantID     string    `json:"tenant_id"`
	GeneratedAt  time.Time `json:"generated_at"`
	AuthzVersion int64     `json:"authz_version"`
	Items        []T       `json:"items"`
	NextCursor   *string   `json:"next_cursor"`
}
type ManagementEvent struct {
	ID                string    `json:"id"`
	ActorMembershipID string    `json:"actor_membership_id"`
	Action            string    `json:"action"`
	TargetID          string    `json:"target_id"`
	OccurredAt        time.Time `json:"occurred_at"`
	RoleID            *string   `json:"role_id"`
	RoleCode          *string   `json:"role_code"`
	MembershipID      *string   `json:"membership_id"`
}
type MachineEvent struct {
	ID                 string    `json:"id"`
	ApplicationID      string    `json:"application_id"`
	ServicePrincipalID string    `json:"service_principal_id"`
	CredentialID       string    `json:"credential_id"`
	Action             string    `json:"action"`
	OccurredAt         time.Time `json:"occurred_at"`
}

// Exact keys (including case), duplicates and whole-document validation prevent
// new metadata or private fields silently entering a versioned audit consumer.
func object(body []byte, fields map[string]func(json.RawMessage) error) error {
	d := json.NewDecoder(bytes.NewReader(body))
	start, e := d.Token()
	if e != nil || start != json.Delim('{') {
		return ErrInvalidResponse
	}
	seen := map[string]bool{}
	for d.More() {
		key, e := d.Token()
		name, ok := key.(string)
		if e != nil || !ok || seen[name] || fields[name] == nil {
			return ErrInvalidResponse
		}
		seen[name] = true
		var raw json.RawMessage
		if d.Decode(&raw) != nil || fields[name](raw) != nil {
			return ErrInvalidResponse
		}
	}
	end, e := d.Token()
	if e != nil || end != json.Delim('}') || len(seen) != len(fields) {
		return ErrInvalidResponse
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrInvalidResponse
	}
	return nil
}
func value[T any](p *T) func(json.RawMessage) error {
	return func(raw json.RawMessage) error { return json.Unmarshal(raw, p) }
}
func decodeManagement(body []byte) (v ManagementEvent, err error) {
	err = object(body, map[string]func(json.RawMessage) error{"id": value(&v.ID), "actor_membership_id": value(&v.ActorMembershipID), "action": value(&v.Action), "target_id": value(&v.TargetID), "occurred_at": value(&v.OccurredAt), "role_id": value(&v.RoleID), "role_code": value(&v.RoleCode), "membership_id": value(&v.MembershipID)})
	if err != nil || !idPattern.MatchString(v.ID) || !idPattern.MatchString(v.ActorMembershipID) || !idPattern.MatchString(v.TargetID) || !actionPattern.MatchString(v.Action) || v.OccurredAt.IsZero() {
		return ManagementEvent{}, ErrInvalidResponse
	}
	if v.RoleID == nil {
		if v.RoleCode != nil || v.MembershipID != nil {
			return ManagementEvent{}, ErrInvalidResponse
		}
	} else if !idPattern.MatchString(*v.RoleID) || v.RoleCode == nil || len(*v.RoleCode) < 1 || len(*v.RoleCode) > 64 || v.MembershipID == nil || !idPattern.MatchString(*v.MembershipID) {
		return ManagementEvent{}, ErrInvalidResponse
	}
	return v, nil
}
func decodeMachine(body []byte) (v MachineEvent, err error) {
	err = object(body, map[string]func(json.RawMessage) error{"id": value(&v.ID), "application_id": value(&v.ApplicationID), "service_principal_id": value(&v.ServicePrincipalID), "credential_id": value(&v.CredentialID), "action": value(&v.Action), "occurred_at": value(&v.OccurredAt)})
	if err != nil || !idPattern.MatchString(v.ID) || !idPattern.MatchString(v.ApplicationID) || !idPattern.MatchString(v.ServicePrincipalID) || !idPattern.MatchString(v.CredentialID) || v.Action != "token.client_credentials" || v.OccurredAt.IsZero() {
		return MachineEvent{}, ErrInvalidResponse
	}
	return v, nil
}
func decodeBatch[T any](body []byte, tenant, kind string, page Page, decode func([]byte) (T, error), id func(T) string) (v Batch[T], err error) {
	items := func(raw json.RawMessage) error {
		d := json.NewDecoder(bytes.NewReader(raw))
		start, e := d.Token()
		if e != nil || start != json.Delim('[') {
			return ErrInvalidResponse
		}
		v.Items = []T{}
		last := page.After
		for d.More() {
			if len(v.Items) >= page.Limit {
				return ErrInvalidResponse
			}
			var row json.RawMessage
			if d.Decode(&row) != nil {
				return ErrInvalidResponse
			}
			record, e := decode(row)
			if e != nil || id(record) <= last {
				return ErrInvalidResponse
			}
			last = id(record)
			v.Items = append(v.Items, record)
		}
		end, e := d.Token()
		if e != nil || end != json.Delim(']') {
			return ErrInvalidResponse
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return ErrInvalidResponse
		}
		return nil
	}
	err = object(body, map[string]func(json.RawMessage) error{"format": value(&v.Format), "kind": value(&v.Kind), "tenant_id": value(&v.TenantID), "generated_at": value(&v.GeneratedAt), "authz_version": value(&v.AuthzVersion), "items": items, "next_cursor": value(&v.NextCursor)})
	if err != nil || v.Format != "sso.audit.batch.v1" || v.Kind != kind || v.TenantID != tenant || v.GeneratedAt.IsZero() || v.AuthzVersion < 1 {
		return Batch[T]{}, ErrInvalidResponse
	}
	if v.NextCursor != nil && (!idPattern.MatchString(*v.NextCursor) || len(v.Items) != page.Limit || *v.NextCursor != id(v.Items[len(v.Items)-1])) {
		return Batch[T]{}, ErrInvalidResponse
	}
	return v, nil
}
