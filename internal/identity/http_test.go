package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func apiTest(t *testing.T, f *fixture) (*http.Client, string) {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	server.Config.Handler = Handler(f.service, origin, Assets())
	server.Start()
	t.Cleanup(server.Close)
	return server.Client(), origin
}
func call(t *testing.T, client *http.Client, origin, path, body, overrideOrigin, contentType string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest("POST", origin+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	if overrideOrigin != "" {
		req.Header.Set("Origin", overrideOrigin)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header, data
}
func TestHTTPProofLifecycleAndUniformResponses(t *testing.T) {
	f := newFixture(t)
	client, origin := apiTest(t, f)
	body, _ := json.Marshal(map[string]string{"email": f.email})
	status, headers, reply := call(t, client, origin, "/v1/registrations", string(body), origin, "application/json")
	if status != 202 || headers.Get("Cache-Control") != "no-store" || headers.Get("X-Request-ID") == "" {
		t.Fatal("registration HTTP contract")
	}
	if bytes.Contains(reply, []byte("token")) || bytes.Contains(reply, []byte(f.email)) {
		t.Fatal("proof/account detail leaked")
	}
	proof := f.request(t, "register")
	complete, _ := json.Marshal(map[string]string{"token": proof.Token, "password": initialPassword})
	status, _, _ = call(t, client, origin, "/v1/email-verifications", string(complete), origin, "application/json")
	if status != 204 {
		t.Fatal("HTTP verification failed")
	}
	status, _, existing := call(t, client, origin, "/v1/registrations", string(body), origin, "application/json")
	if status != 202 || !bytes.Equal(reply, existing) {
		t.Fatal("registration enumerates existing accounts")
	}
	status, _, _ = call(t, client, origin, "/v1/email-verifications", string(complete), origin, "application/json")
	if status != 400 {
		t.Fatal("HTTP proof replay accepted")
	}
	if _, err := f.service.CheckPassword(context.Background(), f.email, initialPassword); err != nil {
		t.Fatal("HTTP stored password cannot authenticate factor")
	}
}
func TestHTTPRejectsCrossOriginOversizeAndAmbiguousInput(t *testing.T) {
	f := newFixture(t)
	client, origin := apiTest(t, f)
	for _, tc := range []struct {
		origin, kind, body, path string
		want                     int
	}{
		{"https://evil.invalid", "application/json", `{"email":"x@example.invalid"}`, "/v1/registrations", 403},
		{"", "application/json", `{"email":"x@example.invalid"}`, "/v1/registrations", 403},
		{origin, "text/plain", `{"email":"x@example.invalid"}`, "/v1/registrations", 415},
		{origin, "application/json", `{"email":"x@example.invalid","tenant_id":"spoof"}`, "/v1/registrations", 400},
		{origin, "application/json", `{"email":"x@example.invalid"}{}`, "/v1/registrations", 400},
		{origin, "application/json", string([]byte{123, 34, 120, 34, 58, 34, 255, 34, 125}), "/v1/registrations", 400},
		{origin, "application/json", `{"email":"` + strings.Repeat("x", 5000) + `"}`, "/v1/registrations", 400},
		{origin, "application/json", `{}`, "/v1/email-verifications?token=forbidden", 400},
	} {
		status, headers, _ := call(t, client, origin, tc.path, tc.body, tc.origin, tc.kind)
		if status != tc.want {
			t.Fatalf("HTTP status=%d want=%d", status, tc.want)
		}
		if headers.Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("cross-origin access enabled")
		}
	}
}
func TestHTTPLimitsRequestsWithoutAccountEnumeration(t *testing.T) {
	f := newFixture(t)
	client, origin := apiTest(t, f)
	body, _ := json.Marshal(map[string]string{"email": f.email})
	for i := 0; i < 6; i++ {
		status, header, _ := call(t, client, origin, "/v1/password-resets", string(body), origin, "application/json")
		if i < 5 && status != 202 {
			t.Fatal("unexpected request rejection")
		}
		if i == 5 && (status != 429 || header.Get("Retry-After") == "") {
			t.Fatal("email limit missing")
		}
	}
}
func TestLocalLimiterHasBoundedCapacity(t *testing.T) {
	l := NewLimiter()
	if !l.Allow("a", 1, time.Hour) || l.Allow("a", 1, time.Hour) {
		t.Fatal("limit bypass")
	}
	for i := 0; i < 10000; i++ {
		l.entries[string(rune(i+100))] = bucket{1, time.Now().Add(time.Hour)}
	}
	if l.Allow("new-key", 1, time.Hour) {
		t.Fatal("capacity did not fail closed")
	}
}
func TestHashCapacityRefusesWithoutConsumingProof(t *testing.T) {
	f := newFixture(t)
	message := f.request(t, "register")
	f.service.pool <- struct{}{}
	f.service.pool <- struct{}{}
	err := f.service.Complete(context.Background(), message.Token, initialPassword, "register")
	<-f.service.pool
	<-f.service.pool
	if err != ErrBusy {
		t.Fatal("hash capacity not bounded")
	}
	digest, _ := tokenDigest(message.Token)
	if err = f.service.Store.ChallengeUsable(context.Background(), digest, "register", time.Now().Unix()); err != nil {
		t.Fatal("capacity rejection consumed proof")
	}
}
