// identity-api exposes local accounts and optional trusted Hydra interactions.
package main

import (
	"context"
	"encoding/hex"
	"github.com/lth2015/sso/internal/identity"
	"github.com/lth2015/sso/internal/localruntime"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	if os.Getenv("SSO_MODE") != "local-accounts" {
		log.Fatal("local accounts mode must be explicit")
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
		login, err = identity.NewLogin(service, flowKey, mfaKey)
		if err != nil {
			log.Fatal("login initialization failed")
		}
		handler := login.Handler()
		accounts := identity.Handler(service, "http://auth.localhost:8080", identity.Assets())
		fallback := public
		public = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := r.URL.Path
			if p == "/login" || p == "/consent" || p == "/flow" || strings.HasPrefix(p, "/flow/") || strings.HasPrefix(p, "/v1/flows/") || strings.HasPrefix(p, "/v1/sso/") || strings.HasPrefix(p, "/sso/") {
				handler.ServeHTTP(w, r)
			} else if r.Host == "auth.localhost:8080" {
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
	log.Print("local account API ready; no password-to-token endpoint")
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("identity HTTP server failed")
	}
}
