// Package hydra is a narrow adapter for the pinned engine's administrative API.
package hydra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const Issuer = "http://auth.localhost:4464/"
const AuthOrigin = "http://auth.localhost:8080"

type Client struct {
	HTTP *http.Client
	Base string
}

// New targets only the private fixed Admin service. Like the private protocol
// transport, it must not send internal control-plane calls through an outbound
// HTTP proxy or depend on a deployment NO_PROXY list. Browser input never
// selects this destination. Redirects remain forbidden.
func New() *Client {
	return &Client{Base: "http://hydra-login:4445", HTTP: &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) Do(ctx context.Context, method, path string, body, result any) error {
	var input io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return errors.New("protocol request invalid")
		}
		input = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, input)
	if err != nil {
		return errors.New("protocol request invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return errors.New("protocol unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("protocol request rejected")
	}
	if result == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return err
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(result) != nil {
		return errors.New("protocol response invalid")
	}
	return nil
}

type Interaction struct {
	Challenge         string   `json:"challenge"`
	Skip              bool     `json:"skip"`
	Subject           string   `json:"subject"`
	SessionID         string   `json:"session_id"`
	LoginSessionID    string   `json:"login_session_id"`
	RequestURL        string   `json:"request_url"`
	RequestedScopes   []string `json:"requested_scope"`
	RequestedAudience []string `json:"requested_access_token_audience"`
	Client            struct {
		ID string `json:"client_id"`
	} `json:"client"`
}

func (c *Client) Get(ctx context.Context, kind, challenge string) (Interaction, error) {
	var value Interaction
	err := c.Do(ctx, "GET", "/admin/oauth2/auth/requests/"+kind+"?"+url.Values{kind + "_challenge": {challenge}}.Encode(), nil, &value)
	return value, err
}
func (c *Client) Decide(ctx context.Context, kind, challenge, decision string, body any) (string, error) {
	if (kind != "login" && kind != "consent") || (decision != "accept" && decision != "reject") {
		return "", errors.New("protocol decision invalid")
	}
	var response struct {
		Redirect string `json:"redirect_to"`
	}
	err := c.Do(ctx, "PUT", "/admin/oauth2/auth/requests/"+kind+"/"+decision+"?"+url.Values{kind + "_challenge": {challenge}}.Encode(), body, &response)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(response.Redirect)
	if err != nil || parsed.Scheme != "http" || parsed.Host != "auth.localhost:4464" || parsed.Path != "/oauth2/auth" || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("protocol redirect invalid")
	}
	return response.Redirect, nil
}
func ScopeSubset(requested []string, upper string) bool {
	allowed := map[string]bool{}
	for _, scope := range strings.Fields(upper) {
		allowed[scope] = true
	}
	if len(requested) == 0 {
		return false
	}
	for _, scope := range requested {
		if !allowed[scope] {
			return false
		}
	}
	return true
}
