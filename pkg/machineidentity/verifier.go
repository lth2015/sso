// Package machineidentity verifies machine Bearers through the SSO primary-bound
// identity resource. It never caches authorization or accepts User identities.
package machineidentity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ErrConfiguration   = errors.New("machine identity configuration invalid")
	ErrInvalidRequest  = errors.New("machine identity request invalid")
	ErrUnauthenticated = errors.New("machine identity unauthenticated")
	ErrForbidden       = errors.New("machine identity forbidden")
	ErrUnavailable     = errors.New("machine identity authority unavailable")
	ErrInvalidResponse = errors.New("machine identity authority response invalid")
)

const localOrigin = "http://api.localhost:8083"

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var bearerPattern = regexp.MustCompile(`^[A-Za-z0-9._~+/-]+=*$`)

// Config is trusted deployment configuration, never request input. Transport is
// optional for a custom CA or a controlled test; its implementation must not log
// credentials. New always owns redirect policy, cookies and the total timeout.
type Config struct {
	Origin         string
	AllowLocalHTTP bool
	Transport      http.RoundTripper
	Timeout        time.Duration
}

// Identity is authority-checked metadata. The application must still decide
// business resource permissions; machine:invoke grants no platform administration.
type Identity struct {
	Kind               string `json:"principal_type"`
	TenantID           string `json:"tenant_id"`
	ApplicationID      string `json:"application_id"`
	ServicePrincipalID string `json:"service_principal_id"`
	CredentialID       string `json:"credential_id"`
	Scope              string `json:"scope"`
	Audience           string `json:"audience"`
}

type Verifier struct {
	origin string
	client *http.Client
}

func New(cfg Config) (*Verifier, error) {
	u, err := url.Parse(cfg.Origin)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") {
		return nil, ErrConfiguration
	}
	if port := u.Port(); port != "" {
		n, e := strconv.ParseUint(port, 10, 16)
		if e != nil || n == 0 {
			return nil, ErrConfiguration
		}
	}
	origin := u.Scheme + "://" + u.Host
	if u.Scheme != "https" && !(cfg.AllowLocalHTTP && origin == localOrigin) {
		return nil, ErrConfiguration
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.Timeout < 0 || cfg.Timeout > 5*time.Second {
		return nil, ErrConfiguration
	}
	transport := cfg.Transport
	if transport == nil {
		dialer := &net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}
		t := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: dialer.DialContext, TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 2 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 16, MaxIdleConnsPerHost: 8, DisableCompression: true}
		if origin == localOrigin {
			t.Proxy = nil
			t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				if address != "api.localhost:8083" {
					return nil, ErrConfiguration
				}
				return dialer.DialContext(ctx, network, "127.0.0.1:8083")
			}
		}
		transport = t
	}
	return &Verifier{origin: origin, client: &http.Client{Transport: transport, Timeout: cfg.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Verify expects the resource's trusted tenant/application IDs and a raw inbound
// Bearer (without the "Bearer " prefix). It makes an online check on every call.
// It never retries the business action, returns raw upstream errors or logs tokens.
func (v *Verifier) Verify(ctx context.Context, tenant, application, token string) (Identity, error) {
	var empty Identity
	if v == nil || v.client == nil || ctx == nil || !idPattern.MatchString(tenant) || !idPattern.MatchString(application) || len(token) > 16384 || !bearerPattern.MatchString(token) {
		return empty, ErrInvalidRequest
	}
	req, err := http.NewRequestWithContext(ctx, "GET", v.origin+"/v1/machine/applications/"+application+"/identity", nil)
	if err != nil {
		return empty, ErrInvalidRequest
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-store")
	req.Header.Set("Pragma", "no-cache")
	response, err := v.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		return empty, ErrUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return empty, ErrUnauthenticated
	case http.StatusForbidden:
		return empty, ErrForbidden
	case http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusInternalServerError:
		return empty, ErrUnavailable
	case http.StatusOK:
	default:
		return empty, ErrInvalidResponse
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || !hasNoStore(response.Header) || (len(response.Header.Values("Age")) > 1 || (response.Header.Get("Age") != "" && response.Header.Get("Age") != "0")) || (response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity") {
		return empty, ErrInvalidResponse
	}
	const maxBody = 16384
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil {
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		return empty, ErrUnavailable
	}
	if len(body) > maxBody {
		return empty, ErrInvalidResponse
	}
	identity, err := decodeIdentity(body)
	if err != nil || identity.Kind != "service_principal" || identity.TenantID != tenant || identity.ApplicationID != application || !idPattern.MatchString(identity.ServicePrincipalID) || !idPattern.MatchString(identity.CredentialID) || identity.Scope != "machine:invoke" || identity.Audience != "urn:sso:application:"+application {
		return empty, ErrInvalidResponse
	}
	return identity, nil
}

func hasNoStore(header http.Header) bool {
	for _, line := range header.Values("Cache-Control") {
		for _, directive := range strings.Split(line, ",") {
			if strings.EqualFold(strings.TrimSpace(directive), "no-store") {
				return true
			}
		}
	}
	return false
}

func decodeIdentity(body []byte) (Identity, error) {
	var result Identity
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return result, ErrInvalidResponse
	}
	fields := map[string]*string{"principal_type": &result.Kind, "tenant_id": &result.TenantID, "application_id": &result.ApplicationID, "service_principal_id": &result.ServicePrincipalID, "credential_id": &result.CredentialID, "scope": &result.Scope, "audience": &result.Audience}
	seen := map[string]bool{}
	for decoder.More() {
		name, err := decoder.Token()
		if err != nil {
			return result, ErrInvalidResponse
		}
		key, ok := name.(string)
		if !ok || seen[key] || fields[key] == nil {
			return result, ErrInvalidResponse
		}
		seen[key] = true
		if decoder.Decode(fields[key]) != nil {
			return result, ErrInvalidResponse
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || len(seen) != len(fields) {
		return result, ErrInvalidResponse
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return result, ErrInvalidResponse
	}
	return result, nil
}
