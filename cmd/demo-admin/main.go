// Local-only demo enablement uses the real business repository, never a login bypass.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/lth2015/sso/internal/localruntime"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"log"
	"os"
)

func main() {
	email := flag.String("email", "", "verified local account email")
	application := flag.String("application", "local-demo", "owned local application code")
	second := flag.Bool("second-tenant", false, "create a second local demo space")
	flag.Parse()
	if *application != "local-demo" && *application != "local-platform" {
		log.Fatal("unknown local application")
	}
	if os.Getenv("SSO_MODE") != "local-accounts" {
		log.Fatal("local mode required")
	}
	normalized, err := store.NormalizeEmail(*email)
	if err != nil {
		log.Fatal("valid email required")
	}
	db, err := localruntime.Database()
	if err != nil {
		log.Fatal("database unavailable")
	}
	defer db.Close()
	repo := store.NewStore(db)
	proof, err := repo.PasswordCredential(context.Background(), normalized)
	if err != nil {
		log.Fatal("active verified local account required")
	}
	if *second {
		if _, err = repo.BootstrapTenant(context.Background(), proof.User, "demo-"+proof.User.String(), "演示团队空间"); err != nil {
			log.Fatal("second tenant creation failed")
		}
	}
	_, err = db.Exec(`INSERT INTO tenant_applications(tenant_id,application_id) SELECT DISTINCT m.tenant_id,a.id FROM memberships m JOIN role_bindings b ON b.tenant_id=m.tenant_id AND b.membership_id=m.id JOIN roles r ON r.tenant_id=b.tenant_id AND r.id=b.role_id JOIN applications a ON a.code=? AND a.active=TRUE JOIN tenants t ON t.id=m.tenant_id WHERE m.user_id=? AND m.status='active' AND t.status='active' AND r.code='owner' ON DUPLICATE KEY UPDATE security_version=IF(tenant_applications.active=FALSE,tenant_applications.security_version+1,tenant_applications.security_version),active=TRUE`, *application, proof.User)
	if err != nil {
		log.Fatal("local app enablement failed")
	}
	fmt.Println("Local demo enabled for this account's owned spaces")
}
