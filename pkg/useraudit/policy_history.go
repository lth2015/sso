package useraudit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

const PolicyHistoryFormat = "sso.role.policy-history.v1"

var roleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

type RolePolicy struct {
	Permission string `json:"permission"`
	Effect     string `json:"effect"`
}
type PolicyChange struct {
	ID                string       `json:"id"`
	ActorMembershipID string       `json:"actor_membership_id"`
	RoleCode          string       `json:"role_code"`
	Action            string       `json:"action"`
	OccurredAt        time.Time    `json:"occurred_at"`
	AuthzVersion      int64        `json:"authz_version"`
	Before            []RolePolicy `json:"before"`
	After             []RolePolicy `json:"after"`
}
type PolicyHistory struct {
	Format     string         `json:"format"`
	TenantID   string         `json:"tenant_id"`
	RoleID     string         `json:"role_id"`
	Items      []PolicyChange `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}

// PolicyHistory reads one current-owner-authorized page. audit.read delegation
// cannot substitute for current owner/roles.manage, including after deletion.
func (c *Client) PolicyHistory(ctx context.Context, token, tenant, role string, page Page) (PolicyHistory, error) {
	var empty PolicyHistory
	if page.Limit == 0 {
		page.Limit = 50
	}
	if !idPattern.MatchString(tenant) || !idPattern.MatchString(role) || page.Limit < 1 || page.Limit > 100 || page.After != "" && !idPattern.MatchString(page.After) {
		return empty, ErrInvalidRequest
	}
	query := url.Values{"limit": {strconv.Itoa(page.Limit)}}
	if page.After != "" {
		query.Set("after", page.After)
	}
	body, e := c.readJSON(ctx, token, "/v1/tenants/"+tenant+"/roles/"+role+"/policy-history?"+query.Encode())
	if e != nil {
		return empty, e
	}
	result, e := decodePolicyHistory(body, tenant, role, page)
	if e != nil {
		return empty, ErrInvalidResponse
	}
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	return result, nil
}
func boundedArray[T any](raw []byte, maximum int, decode func([]byte) (T, error)) ([]T, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	start, e := d.Token()
	if e != nil || start != json.Delim('[') {
		return nil, ErrInvalidResponse
	}
	result := []T{}
	for d.More() {
		if len(result) >= maximum {
			return nil, ErrInvalidResponse
		}
		var row json.RawMessage
		if d.Decode(&row) != nil {
			return nil, ErrInvalidResponse
		}
		value, e := decode(row)
		if e != nil {
			return nil, ErrInvalidResponse
		}
		result = append(result, value)
	}
	end, e := d.Token()
	if e != nil || end != json.Delim(']') {
		return nil, ErrInvalidResponse
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, ErrInvalidResponse
	}
	return result, nil
}
func decodeRolePolicy(raw []byte) (v RolePolicy, e error) {
	e = object(raw, map[string]func(json.RawMessage) error{"permission": value(&v.Permission), "effect": value(&v.Effect)})
	if e != nil || (v.Effect != "allow" && v.Effect != "deny") {
		return RolePolicy{}, ErrInvalidResponse
	}
	switch v.Permission {
	case "audit.read", "invitations.manage", "members.read", "members.write", "organizations.read", "organizations.write":
	default:
		return RolePolicy{}, ErrInvalidResponse
	}
	return v, nil
}
func policyRules(raw json.RawMessage) ([]RolePolicy, error) {
	result, e := boundedArray(raw, 12, decodeRolePolicy)
	if e != nil {
		return nil, e
	}
	last := ""
	for _, v := range result {
		key := v.Permission + ":" + v.Effect
		if key <= last {
			return nil, ErrInvalidResponse
		}
		last = key
	}
	return result, nil
}
func decodePolicyChange(raw []byte) (v PolicyChange, e error) {
	before := func(raw json.RawMessage) error { var e error; v.Before, e = policyRules(raw); return e }
	after := func(raw json.RawMessage) error { var e error; v.After, e = policyRules(raw); return e }
	e = object(raw, map[string]func(json.RawMessage) error{"id": value(&v.ID), "actor_membership_id": value(&v.ActorMembershipID), "role_code": value(&v.RoleCode), "action": value(&v.Action), "occurred_at": value(&v.OccurredAt), "authz_version": value(&v.AuthzVersion), "before": before, "after": after})
	if e != nil || !idPattern.MatchString(v.ID) || !idPattern.MatchString(v.ActorMembershipID) || !roleCodePattern.MatchString(v.RoleCode) || v.RoleCode == "owner" || v.AuthzVersion < 1 || v.OccurredAt.IsZero() {
		return PolicyChange{}, ErrInvalidResponse
	}
	switch v.Action {
	case "role.created":
		if len(v.Before) != 0 {
			return PolicyChange{}, ErrInvalidResponse
		}
	case "role.deleted":
		if len(v.After) != 0 {
			return PolicyChange{}, ErrInvalidResponse
		}
	case "role.updated":
		equal := len(v.Before) == len(v.After)
		if equal {
			for i := range v.Before {
				if v.Before[i] != v.After[i] {
					equal = false
					break
				}
			}
		}
		if equal {
			return PolicyChange{}, ErrInvalidResponse
		}
	default:
		return PolicyChange{}, ErrInvalidResponse
	}
	return v, nil
}
func decodePolicyHistory(raw []byte, tenant, role string, page Page) (v PolicyHistory, e error) {
	items := func(raw json.RawMessage) error {
		var e error
		v.Items, e = boundedArray(raw, page.Limit, decodePolicyChange)
		return e
	}
	e = object(raw, map[string]func(json.RawMessage) error{"format": value(&v.Format), "tenant_id": value(&v.TenantID), "role_id": value(&v.RoleID), "items": items, "next_cursor": value(&v.NextCursor)})
	if e != nil || v.Format != PolicyHistoryFormat || v.TenantID != tenant || v.RoleID != role {
		return PolicyHistory{}, ErrInvalidResponse
	}
	last := page.After
	for _, item := range v.Items {
		if item.ID <= last {
			return PolicyHistory{}, ErrInvalidResponse
		}
		last = item.ID
	}
	if v.NextCursor != nil && (!idPattern.MatchString(*v.NextCursor) || len(v.Items) != page.Limit || *v.NextCursor != last) {
		return PolicyHistory{}, ErrInvalidResponse
	}
	return v, nil
}
