package hydra

import (
	"errors"
	"net/http"
	"net/url"
	"time"
)

// ProtocolTransport keeps the public issuer identity while connecting to a fixed
// private Service. JWT headers and request inputs cannot choose another host.
type ProtocolTransport struct {
	Issuer, Upstream string
	Base             http.RoundTripper
}

func (t ProtocolTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	issuer, e1 := url.Parse(t.Issuer)
	target, e2 := url.Parse(t.Upstream)
	if e1 != nil || e2 != nil || issuer.Host == "" || target.Host == "" || r.URL.Scheme != issuer.Scheme || r.URL.Host != issuer.Host || r.URL.User != nil || r.URL.RawPath != "" {
		return nil, errors.New("untrusted issuer destination")
	}
	c := r.Clone(r.Context())
	u := *r.URL
	c.URL = &u
	c.URL.Scheme, c.URL.Host = target.Scheme, target.Host
	c.Host = ""
	return t.Base.RoundTrip(c)
}
func ValidateEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errors.New("invalid endpoint configuration")
	}
	return nil
}
func ProtocolClient(issuer, upstream string) (*http.Client, error) {
	if ValidateEndpoint(issuer) != nil || ValidateEndpoint(upstream) != nil {
		return nil, errors.New("invalid protocol endpoint")
	}
	return &http.Client{Transport: ProtocolTransport{issuer, upstream, &http.Transport{Proxy: nil, ResponseHeaderTimeout: 3 * time.Second}}, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}
