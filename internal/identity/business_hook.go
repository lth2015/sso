package identity

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/lth2015/sso/internal/adapter/hydra"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"io"
	"net/http"
	"strconv"
	"time"
)

func BusinessHook(repo store.Store, key string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "POST" || r.URL.Path != "/token-hook" {
			w.WriteHeader(404)
			return
		}
		if len(key) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Hook-Key")), []byte(key)) != 1 {
			w.WriteHeader(401)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 65536)
		decoder := json.NewDecoder(r.Body)
		var req hydra.TokenHookRequest
		var extra any
		if decoder.Decode(&req) != nil || decoder.Decode(&extra) != io.EOF {
			w.WriteHeader(400)
			return
		}
		if len(req.Request.GrantTypes) == 1 && req.Request.GrantTypes[0] == "client_credentials" {
			ctx, cancel := context.WithTimeout(r.Context(), time.Second)
			defer cancel()
			claims, err := repo.CheckMachineToken(ctx, store.MachineTokenCheck{Client: req.Request.ClientID, Requested: req.Request.RequestedScopes, Granted: req.Request.GrantedScopes, Audience: req.Request.GrantedAudience, PayloadScopes: req.Request.Payload.Scopes, PayloadAudience: req.Request.Payload.Audiences})
			if err != nil {
				if errors.Is(err, store.ErrDenied) {
					w.WriteHeader(403)
				} else {
					w.WriteHeader(503)
				}
				return
			}
			jsonResponse(w, map[string]any{"session": map[string]any{"access_token": map[string]any{"principal_type": "service_principal", "tenant_id": claims.Tenant.String(), "application_id": claims.Application.String(), "principal_id": claims.Principal.String(), "credential_id": claims.Credential.String(), "tenant_authz_version": strconv.FormatUint(claims.TenantVersion, 10), "application_version": strconv.FormatUint(claims.ApplicationVersion, 10), "enabled_version": strconv.FormatUint(claims.EnabledVersion, 10), "principal_version": strconv.FormatUint(claims.PrincipalVersion, 10), "credential_version": strconv.FormatUint(claims.CredentialVersion, 10), "credential_expires_at": strconv.FormatInt(claims.CredentialExpires, 10)}}})
			return
		}
		value, ok := req.Session.Extra["business_context_id"].(string)
		id, err := store.ParseID(value)
		if !ok || err != nil || len(req.Request.GrantTypes) != 1 {
			w.WriteHeader(403)
			return
		}
		sid, _ := req.Session.IDToken.Claims.Extra["sid"].(string)
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		claims, err := repo.CheckBusinessToken(ctx, store.TokenCheck{GrantID: id, Client: req.Request.ClientID, Subject: req.Session.IDToken.Subject, HydraSID: sid, Binding: req.Session.ConsentChallenge, Grant: req.Request.GrantTypes[0], Requested: req.Request.RequestedScopes, Granted: req.Request.GrantedScopes, Audience: req.Request.GrantedAudience, PayloadScopes: req.Request.Payload.Scopes, PayloadAudience: req.Request.Payload.Audiences})
		if err != nil {
			if errors.Is(err, store.ErrDenied) {
				w.WriteHeader(403)
			} else {
				w.WriteHeader(503)
			}
			return
		}
		jsonResponse(w, map[string]any{"session": map[string]any{"access_token": map[string]any{"business_context_id": claims.Grant.String(), "tenant_id": claims.Tenant.String(), "principal_id": claims.User.String(), "principal_type": "user", "membership_id": claims.Member.String(), "session_id": claims.Session.String(), "user_version": claims.UserVersion, "membership_version": claims.MemberVersion, "tenant_authz_version": claims.TenantVersion}, "id_token": map[string]any{"sid": claims.HydraSID}}})
	})
}
