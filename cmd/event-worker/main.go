// event-worker currently delivers account notifications to a private local mailbox.
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
	"syscall"
	"time"
)

func main() {
	once := flag.Bool("once", false, "drain currently eligible local notifications then exit")
	flag.Parse()
	if os.Getenv("SSO_MODE") != "local-accounts" {
		log.Fatal("local accounts mode must be explicit")
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
	for ctx.Err() == nil {
		step, cancel := context.WithTimeout(ctx, 5*time.Second)
		didWork, err := repo.DeliverOne(step, sink)
		cancel()
		if err != nil {
			log.Print("local notification delivery failed: " + identity.DeliveryFailureCode(err))
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
