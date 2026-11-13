package main

import (
	"context"
	"encoding/hex"
	"github.com/lth2015/sso/internal/demo"
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
	mode := os.Getenv("SSO_MODE")
	if mode != "local-accounts" && mode != "kubernetes" {
		log.Fatal("SSO_MODE must be local-accounts or kubernetes")
	}
	client, secret := os.Getenv("DEMO_CLIENT_ID"), os.Getenv("DEMO_CLIENT_SECRET")
	var runtime []demo.RuntimeConfig
	if mode == "kubernetes" {
		cfg := demo.RuntimeConfig{Origin: os.Getenv("CONSOLE_ORIGIN"), Issuer: os.Getenv("OIDC_ISSUER"), HydraPublicURL: os.Getenv("HYDRA_PUBLIC_URL"), PlatformAPIURL: os.Getenv("PLATFORM_API_URL")}
		if err := cfg.Validate(); err != nil {
			log.Fatal("invalid BFF endpoint configuration")
		}
		client, secret = os.Getenv("OAUTH_CLIENT_ID"), os.Getenv("OAUTH_CLIENT_SECRET")
		if client == "" || len(secret) < 32 {
			log.Fatal("missing BFF OAuth credentials")
		}
		runtime = append(runtime, cfg)
	}
	db, err := localruntime.Database()
	if err != nil {
		log.Fatal("database unavailable")
	}
	defer db.Close()
	key, err := hex.DecodeString(os.Getenv("SSO_BFF_KEY"))
	if err != nil {
		log.Fatal("BFF key unavailable")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var app *demo.App
	for attempt := 0; attempt < 10; attempt++ {
		app, err = demo.New(ctx, store.NewStore(db), client, secret, key, runtime...)
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
		log.Fatal("BFF initialization failed: ", err)
	}
	server := &http.Server{Addr: ":3000", Handler: app.Handler(), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 12 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32768}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Print("BFF ready")
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("BFF server failed")
	}
}
