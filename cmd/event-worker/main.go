// event-worker delivers queued account notifications.
package main

import (
	"context"
	"flag"
	"github.com/lth2015/sso/internal/identity"
	"github.com/lth2015/sso/internal/localruntime"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	once := flag.Bool("once", false, "drain currently eligible notifications then exit")
	checkSMTP := flag.Bool("check-smtp", false, "verify SMTP TLS and authentication without sending email")
	flag.Parse()
	mode := os.Getenv("SSO_MODE")
	if mode != "local-accounts" && mode != "kubernetes" {
		log.Fatal("worker mode must be explicit")
	}
	db, err := localruntime.Database()
	if err != nil {
		log.Fatal("database unavailable")
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	key, err := localruntime.MailKey()
	if err != nil {
		log.Fatal("mail key unavailable")
	}
	aead, err := identity.NewCipher(key)
	if err != nil {
		log.Fatal("mail cipher unavailable")
	}
	repo := store.NewStore(db)
	sink := identity.FileSink("/mailbox", aead)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if mode == "kubernetes" {
		port, _ := strconv.Atoi(os.Getenv("SMTP_PORT"))
		cfg := identity.SMTPConfig{Host: os.Getenv("SMTP_HOST"), Port: port, From: os.Getenv("SMTP_FROM"), Username: os.Getenv("SMTP_USERNAME"), Password: os.Getenv("SMTP_PASSWORD"), AccountURL: os.Getenv("MAIL_ACCOUNT_URL")}
		if err := cfg.Validate(); err != nil {
			log.Fatal("invalid SMTP configuration")
		}
		if *checkSMTP {
			if err := cfg.Check(ctx); err != nil {
				log.Fatal("SMTP check failed: " + identity.DeliveryFailureCode(err))
			}
			log.Print("SMTP TLS and authentication verified; no email sent")
			return
		}
		sink = cfg.Sink(ctx, aead)
	}
	log.Print("notification worker ready")
	for ctx.Err() == nil {
		step, cancel := context.WithTimeout(ctx, 20*time.Second)
		didWork, err := repo.DeliverOne(step, sink)
		cancel()
		if err != nil {
			log.Print("notification delivery failed: " + identity.DeliveryFailureCode(err))
			if *once {
				os.Exit(1)
			}
		}
		if *once && !didWork {
			return
		}
		if !didWork || err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}
