package demo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	store "github.com/lth2015/sso/internal/storage/mysql"
)

// sessionStorage keeps the existing local reference API while allowing a strictly
// isolated database implementation for Kubernetes deployments.
type sessionStorage interface {
	CreateBFFTransaction(context.Context, []byte, []byte, string, []byte) error
	ConsumeBFFTransaction(context.Context, []byte, []byte, string) ([]byte, error)
	CreateBFFSession(context.Context, []byte, string, store.ID, store.ID, []byte) error
	BFFSession(context.Context, []byte, string) (store.BFFSession, error)
	RefreshBFF(context.Context, []byte, string, func(store.BFFSession) ([]byte, error)) error
	RevokeBFF(context.Context, []byte, string) error
	BFFStorageReady(context.Context, string) error
}

func (a *App) sessions() sessionStorage {
	if a.Sessions != nil {
		return a.Sessions
	}
	return a.Store
}

var errAuthorityUnavailable = errors.New("authority unavailable")

func (a *App) authorityCall(ctx context.Context, method, path, authorization string, body any, out any) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return errAuthorityUnavailable
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, a.platformURL()+path, bytes.NewReader(data))
	if err != nil {
		return errAuthorityUnavailable
	}
	request.Host = "api.localhost:8083"
	request.Header.Set("Authorization", authorization)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := authorityHTTP.Do(request)
	if err != nil {
		return errAuthorityUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == 410 && path == "/internal/bff/revoke" {
		return store.ErrBFFRevocationExpired
	}
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return store.ErrDenied
	}
	if response.StatusCode != 200 && response.StatusCode != 201 && response.StatusCode != 204 {
		return errAuthorityUnavailable
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(data) > 8192 {
		return errAuthorityUnavailable
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return errAuthorityUnavailable
	}
	return nil
}

var authorityHTTP = &http.Client{Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 3 * time.Second}, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (a *App) checkAuthority(ctx context.Context, access string, expected accessClaims) error {
	var current struct{ User, Tenant, Grant, Member, Session, Client string }
	if err := a.authorityCall(ctx, "GET", "/internal/bff/authorization", "Bearer "+access, nil, &current); err != nil {
		return err
	}
	if current.User != expected.Subject || current.Tenant != expected.Ext.Tenant || current.Grant != expected.Ext.Grant || current.Member != expected.Ext.Member || current.Session != expected.Ext.Session || current.Client != a.Client {
		return store.ErrDenied
	}
	return nil
}
func (a *App) issueRevocation(ctx context.Context, access string) (string, int64, error) {
	var result struct {
		Credential string `json:"credential"`
		Expires    int64  `json:"expires_at"`
	}
	if err := a.authorityCall(ctx, "POST", "/internal/bff/revocation-credentials", "Bearer "+access, struct{}{}, &result); err != nil {
		return "", 0, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(result.Credential)
	if err != nil || len(raw) != 32 || result.Expires <= time.Now().Unix() {
		return "", 0, errAuthorityUnavailable
	}
	return result.Credential, result.Expires, nil
}
func (a *App) deliverRevocation(ctx context.Context) (bool, error) {
	isolated, ok := a.Sessions.(store.IsolatedBFFStore)
	if !ok {
		return false, nil
	}
	return isolated.DeliverOneRevocation(ctx, a.Client, func(digest, payload []byte, reason string) error {
		var tokens Tokens
		if a.open(payload, a.sessionAAD(digest), &tokens) != nil || tokens.Revocation == "" {
			return errors.New("invalid encrypted revocation job")
		}
		if tokens.RevocationExpires <= time.Now().Unix() {
			return store.ErrBFFRevocationExpired
		}
		return a.authorityCall(ctx, "POST", "/internal/bff/revoke", "BFF-Revoke "+tokens.Revocation, map[string]string{"reason": reason}, nil)
	})
}
func (a *App) ReconcileRevocations(ctx context.Context) {
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	for {
		step, cancel := context.WithTimeout(ctx, 6*time.Second)
		if isolated, ok := a.Sessions.(store.IsolatedBFFStore); ok {
			if _, err := isolated.ExpireOneBFFSession(step, a.Client); err != nil {
				log.Print("BFF expired session cleanup pending")
			}
		}
		did, err := a.deliverRevocation(step)
		cancel()
		if err != nil {
			log.Print("BFF authorization revocation pending; retry retained")
		}
		if did && err == nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
func (a *App) flushRevocation(ctx context.Context) {
	if a.Console {
		_, _ = a.deliverRevocation(ctx)
	}
}
