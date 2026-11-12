// Package useraudit reads one primary-authorized User audit batch per call.
// It obtains no credentials, caches no authorization and never follows redirects.
package useraudit

import (
	"context"
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
	"unicode/utf8"
)

var (
	ErrConfiguration   = errors.New("user audit configuration invalid")
	ErrInvalidRequest  = errors.New("user audit request invalid")
	ErrUnauthenticated = errors.New("user audit unauthenticated")
	ErrForbidden       = errors.New("user audit forbidden")
	ErrNotFound        = errors.New("user audit resource or cursor not found")
	ErrUnavailable     = errors.New("user audit authority unavailable")
	ErrInvalidResponse = errors.New("user audit authority response invalid")
)

const localOrigin = "http://api.localhost:8083"

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var bearerPattern = regexp.MustCompile(`^[A-Za-z0-9._~+/-]+=*$`)
var actionPattern = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)

// Config is trusted deployment configuration, not request input. A custom
// Transport must preserve TLS trust and must never log credentials.
type Config struct {
	Origin         string
	AllowLocalHTTP bool
	Transport      http.RoundTripper
	Timeout        time.Duration
}
type Client struct {
	origin string
	client *http.Client
}

// Page selects one bounded keyset batch. Limit zero defaults to 50.
type Page struct {
	Limit int
	After string
}

func New(cfg Config) (*Client, error) {
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
	return &Client{origin: origin, client: &http.Client{Transport: transport, Timeout: cfg.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) Management(ctx context.Context, token, tenant string, page Page) (Batch[ManagementEvent], error) {
	return fetchBatch(ctx, c, token, tenant, "management", page, decodeManagement, func(v ManagementEvent) string { return v.ID })
}
func (c *Client) Machine(ctx context.Context, token, tenant string, page Page) (Batch[MachineEvent], error) {
	return fetchBatch(ctx, c, token, tenant, "machine", page, decodeMachine, func(v MachineEvent) string { return v.ID })
}

func fetchBatch[T any](ctx context.Context, c *Client, token, tenant, kind string, page Page, decode func([]byte) (T, error), id func(T) string) (Batch[T], error) {
	var empty Batch[T]
	if page.Limit == 0 {
		page.Limit = 50
	}
	if c == nil || c.client == nil || ctx == nil || !idPattern.MatchString(tenant) || len(token) > 16384 || !bearerPattern.MatchString(token) || page.Limit < 1 || page.Limit > 100 || (page.After != "" && !idPattern.MatchString(page.After)) {
		return empty, ErrInvalidRequest
	}
	query := url.Values{"kind": {kind}, "limit": {strconv.Itoa(page.Limit)}}
	if page.After != "" {
		query.Set("after", page.After)
	}
	body, err := c.readJSON(ctx, token, "/v1/tenants/"+tenant+"/audit-exports?"+query.Encode())
	if err != nil {
		return empty, err
	}
	result, err := decodeBatch(body, tenant, kind, page, decode, id)
	if err != nil {
		return empty, ErrInvalidResponse
	}
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	return result, nil
}

// readJSON has no caller-controlled destination, redirect or credential cache.
// Only the fixed public methods construct its path. A failed read returns no body.
func (c *Client) readJSON(ctx context.Context, token, path string) ([]byte, error) {
	if c == nil || c.client == nil || ctx == nil || len(token) > 16384 || !bearerPattern.MatchString(token) {
		return nil, ErrInvalidRequest
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.origin+path, nil)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Cache-Control", "no-store")
	req.Header.Set("Pragma", "no-cache")
	response, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	switch response.StatusCode {
	case 401:
		return nil, ErrUnauthenticated
	case 403:
		return nil, ErrForbidden
	case 404:
		return nil, ErrNotFound
	case 429, 500, 502, 503, 504:
		return nil, ErrUnavailable
	case 200:
	default:
		return nil, ErrInvalidResponse
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || response.Uncompressed || !hasNoStore(response.Header) || len(response.Header.Values("Age")) > 1 || (response.Header.Get("Age") != "" && response.Header.Get("Age") != "0") || (response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity") {
		return nil, ErrInvalidResponse
	}
	const maximum = 256 << 10
	body, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	if len(body) > maximum || !utf8.Valid(body) {
		return nil, ErrInvalidResponse
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return body, nil
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
