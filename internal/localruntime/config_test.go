package localruntime

import (
	"crypto/tls"
	"encoding/pem"
	driver "github.com/go-sql-driver/mysql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestKubernetesDatabaseRequiresVerifiedTLS(t *testing.T) {
	for k, v := range map[string]string{"DB_HOST": "database.example.com", "DB_PORT": "3306", "DB_NAME": "sso_business", "DB_USER": "sso_business", "DB_PASSWORD": "test-password"} {
		t.Setenv(k, v)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	t.Setenv("DB_TLS_CA_FILE", path)
	if _, err := kubernetesDatabaseConfig(); err == nil {
		t.Fatal("accepted missing CA")
	}
	os.WriteFile(path, []byte("invalid certificate"), 0600)
	if _, err := kubernetesDatabaseConfig(); err == nil {
		t.Fatal("accepted invalid CA")
	}
}

func TestKubernetesDatabasePreservesTLSAndCredential(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"DB_HOST": "database.example.com", "DB_PORT": "3306", "DB_NAME": "sso_business", "DB_USER": "sso_business", "DB_PASSWORD": "test-special!@:/password", "DB_TLS_CA_FILE": path} {
		t.Setenv(k, v)
	}
	cfg, err := kubernetesDatabaseConfig()
	if err != nil {
		t.Fatal(err)
	}
	_, err = driver.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	actual := cfg
	if actual.TLS == nil || actual.TLS.InsecureSkipVerify || actual.TLS.ServerName != "database.example.com" || actual.TLS.RootCAs == nil || actual.TLS.MinVersion != tls.VersionTLS12 {
		t.Fatal("verified TLS was not preserved")
	}
	if actual.Passwd != "test-special!@:/password" || actual.User != "sso_business" || actual.DBName != "sso_business" {
		t.Fatal("configured credentials were not preserved")
	}
}
