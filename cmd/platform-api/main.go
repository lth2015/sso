package main

import (
	"context"
	"github.com/lth2015/sso/internal/localruntime"
	"github.com/lth2015/sso/internal/platformapi"
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
		log.Fatal("explicit local mode required")
	}
	db, err := localruntime.Database()
	if err != nil {
		log.Fatal("database unavailable")
	}
	defer db.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	var auth *platformapi.Authenticator
	for attempt := 0; attempt < 30; attempt++ {
		auth, err = platformapi.NewAuthenticator(ctx, store.NewStore(db))
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
	if err != nil {
		log.Fatal("protocol verifier unavailable")
	}
	server := &http.Server{Addr: ":8083", Handler: platformapi.Handler(auth), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32768}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	log.Print("local platform API ready")
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("platform API server failed")
	}
}
