// identity-api currently exposes only the local account proof slice.
package main

import (
	"context"
	"github.com/lth2015/sso/internal/identity"
	"github.com/lth2015/sso/internal/localruntime"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	server := &http.Server{Addr: ":8080", Handler: identity.Handler(service, "http://127.0.0.1:8080", identity.Assets()), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Print("local account API ready; no password-to-token endpoint")
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("identity HTTP server failed")
	}
}
