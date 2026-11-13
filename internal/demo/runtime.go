package demo

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// RuntimeConfig describes the fixed, operator-controlled production destinations.
// Browser inputs and JWT headers never choose upstream servers.
type RuntimeConfig struct {
	Origin, Issuer, HydraPublicURL, PlatformAPIURL string
}

func (c RuntimeConfig) Validate() error {
	for i, raw := range []string{c.Origin, c.Issuer, c.HydraPublicURL, c.PlatformAPIURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("invalid BFF endpoint configuration")
		}
		if i < 2 && u.Scheme != "https" {
			return errors.New("BFF public endpoints require HTTPS")
		}
	}
	return nil
}
func (a *App) isConsole() bool { return a.Console || a.Client == "local-console" }
func (a *App) cookieName(name string) string {
	if !strings.HasPrefix(a.Origin, "https://") {
		return name
	}
	switch name {
	case browserCookie:
		return "__Host-sso-bff-browser"
	case sessionCookie:
		return "__Host-sso-bff-session"
	}
	if strings.HasPrefix(name, consoleCookiePrefix) {
		return "__Host-sso-bff-context-" + strings.TrimPrefix(name, consoleCookiePrefix)
	}
	return name
}
func (a *App) consolePrefix() string { return a.cookieName(consoleCookiePrefix) }
func (a *App) setCookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: a.cookieName(name), Value: value, MaxAge: age, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(a.Origin, "https://"), SameSite: http.SameSiteLaxMode})
}
func (a *App) platformURL() string {
	if a.PlatformURL != "" {
		return strings.TrimSuffix(a.PlatformURL, "/")
	}
	return "http://platform-api:8083"
}
