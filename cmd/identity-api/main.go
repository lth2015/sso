// identity-api exposes accounts and trusted Hydra interactions in local or Kubernetes mode.
package main

import (
	"context"
	"encoding/hex"
	"github.com/lth2015/sso/internal/identity"
	"github.com/lth2015/sso/internal/localruntime"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	mode := os.Getenv("SSO_MODE")
	if mode != "local-accounts" && mode != "kubernetes" {
		log.Fatal("SSO_MODE must be local-accounts or kubernetes")
	}
	cfg := identity.LoginConfig{Issuer: "http://auth.localhost:4464/", Origin: "http://auth.localhost:8080", AdminURL: "http://hydra-login:4445", ConsoleOrigin: "http://console.localhost:3003"}
	if mode == "kubernetes" {
		cfg = identity.LoginConfig{Issuer: os.Getenv("OIDC_ISSUER"), Origin: os.Getenv("AUTH_ORIGIN"), AdminURL: os.Getenv("HYDRA_ADMIN_URL"), ConsoleOrigin: os.Getenv("CONSOLE_ORIGIN")}
		if err := cfg.Validate(); err != nil {
			log.Fatal("invalid identity endpoint configuration")
		}
		if !strings.HasPrefix(cfg.Issuer, "https://") || !strings.HasPrefix(cfg.Origin, "https://") || !strings.HasPrefix(cfg.ConsoleOrigin, "https://") || os.Getenv("SSO_ENABLED") != "true" {
			log.Fatal("Kubernetes mode requires HTTPS public endpoints and SSO")
		}
	}
	db, err := localruntime.Database()
	if err != nil {
		log.Fatal("database unavailable")
	}
	defer db.Close()
	key, err := localruntime.MailKey()
	if err != nil {
		log.Fatal("mail key unavailable")
	}
	service, err := identity.New(store.NewStore(db), key)
	if err != nil {
		log.Fatal("identity service initialization failed")
	}
	var private *http.Server
	var login *identity.Login
	public := identity.Handler(service, "http://127.0.0.1:8080", identity.Assets())
	if os.Getenv("SSO_ENABLED") == "true" {
		flowKey, err := hex.DecodeString(os.Getenv("SSO_FLOW_KEY"))
		if err != nil {
			log.Fatal("flow key unavailable")
		}
		mfaKey, err := hex.DecodeString(os.Getenv("SSO_MFA_KEY"))
		if err != nil {
			log.Fatal("MFA key unavailable")
		}
		login, err = identity.NewLoginWithConfig(service, flowKey, mfaKey, cfg)
		if err != nil {
			log.Fatal("login initialization failed")
		}
		handler := login.Handler()
		accounts := identity.Handler(service, strings.TrimSuffix(cfg.Origin, "/"), identity.Assets())
		fallback := public
		public = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := r.URL.Path
			if p == "/login" || p == "/consent" || p == "/flow" || strings.HasPrefix(p, "/flow/") || strings.HasPrefix(p, "/v1/flows/") || strings.HasPrefix(p, "/v1/sso/") || strings.HasPrefix(p, "/sso/") {
				handler.ServeHTTP(w, r)
			} else if origin, _ := url.Parse(cfg.Origin); r.Host == origin.Host {
				accounts.ServeHTTP(w, r)
			} else {
				fallback.ServeHTTP(w, r)
			}
		})
		hookKey := os.Getenv("BUSINESS_HOOK_KEY")
		if len(hookKey) < 32 {
			log.Fatal("business hook key unavailable")
		}
		private = &http.Server{Addr: ":8082", Handler: identity.BusinessHook(store.NewStore(db), hookKey), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
		go func() {
			if err := private.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatal("private business hook failed")
			}
		}()
	}
	probe := identity.Handler(service, strings.TrimSuffix(cfg.Origin, "/"), identity.Assets())
	application := public
	public = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" && r.Method == "GET" {
			w.WriteHeader(204)
			return
		}
		// Probe path is internal-only at ingress; no forwarded Host trust is needed.
		if r.URL.Path == "/health/ready" && r.Method == "GET" {
			probe.ServeHTTP(w, r)
			return
		}
		if mode == "kubernetes" {
			origin, _ := url.Parse(cfg.Origin)
			if r.Host != origin.Host {
				http.Error(w, "invalid_origin", 403)
				return
			}
		}
		application.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: ":8080", Handler: public, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32768}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if login != nil {
		go login.ReconcileLogout(ctx)
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
		if private != nil {
			_ = private.Shutdown(shutdown)
		}
	}()
	log.Print("identity API ready; no password-to-token endpoint")
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("identity HTTP server failed")
	}
}
