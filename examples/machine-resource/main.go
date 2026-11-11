// This loopback-only reference resource verifies machine identities online. It
// intentionally grants no business write operation and accepts no User cookies.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/lth2015/sso/api"
	"github.com/lth2015/sso/pkg/machineidentity"
)

type identityVerifier interface {
	Verify(context.Context, string, string, string) (machineidentity.Identity, error)
}

func handler(verifier identityVerifier, tenant, application string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		fail := func(status int, code string) {
			if status == 401 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="example-machine-resource", error="invalid_token"`)
			}
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]string{"error": code})
		}
		if r.URL.Path == "/openapi.yaml" || r.URL.Path == "/openapi.json" {
			if r.Method != "GET" {
				w.Header().Set("Allow", "GET")
				fail(405, "method_not_allowed")
				return
			}
			if r.URL.RawQuery != "" || r.URL.RawPath != "" {
				fail(400, "invalid_request")
				return
			}
			if r.URL.Path == "/openapi.yaml" {
				w.Header().Set("Content-Type", "application/yaml")
				w.Write(api.MachineResourceExampleSpec)
			} else {
				w.Write(api.MachineResourceExampleJSON)
			}
			return
		}
		if r.URL.Path != "/whoami" || r.URL.RawPath != "" {
			fail(404, "not_found")
			return
		}
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			fail(405, "method_not_allowed")
			return
		}
		if r.URL.RawQuery != "" || r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
			fail(400, "invalid_request")
			return
		}
		values := r.Header.Values("Authorization")
		if len(values) != 1 {
			fail(401, "unauthenticated")
			return
		}
		parts := strings.Fields(values[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			fail(401, "unauthenticated")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		identity, err := verifier.Verify(ctx, tenant, application, parts[1])
		if err != nil {
			switch {
			case errors.Is(err, machineidentity.ErrUnauthenticated), errors.Is(err, machineidentity.ErrInvalidRequest):
				fail(401, "unauthenticated")
			case errors.Is(err, machineidentity.ErrForbidden):
				fail(403, "forbidden")
			default:
				fail(503, "identity_unavailable")
			}
			return
		}
		// Use this verified identity in your own business authorization policy. This
		// reference endpoint only returns metadata and never logs the inbound Bearer.
		json.NewEncoder(w).Encode(identity)
	})
}
func main() {
	tenant, application := os.Getenv("SSO_EXPECTED_TENANT_ID"), os.Getenv("SSO_EXPECTED_APPLICATION_ID")
	id := regexp.MustCompile(`^[a-f0-9]{32}$`)
	if !id.MatchString(tenant) || !id.MatchString(application) {
		log.Fatal("machine resource expected tenant/application configuration invalid")
	}
	verifier, err := machineidentity.New(machineidentity.Config{Origin: os.Getenv("SSO_API_ORIGIN"), AllowLocalHTTP: os.Getenv("SSO_ALLOW_LOCAL_HTTP") == "true"})
	if err != nil {
		log.Fatal("machine resource authority configuration invalid")
	}
	server := &http.Server{Addr: "127.0.0.1:8092", Handler: handler(verifier, tenant, application), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 * 1024}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		log.Fatal("reference machine resource listener unavailable")
	}
	log.Print("reference machine resource ready on 127.0.0.1:8092")
	if err = server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("reference machine resource stopped")
	}
}
