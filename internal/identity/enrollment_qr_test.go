package identity

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"net/url"
	"strings"
	"testing"
)

func TestEnrollmentQRUsesCurrentTOTPProfile(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	account := "person+tag@example.com"
	u, err := url.Parse(provisioningURI(secret, account))
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" || u.Path != "/NSS SSO:"+account {
		t.Fatal("invalid authenticator label")
	}
	for k, want := range map[string]string{"secret": secret, "issuer": "NSS SSO", "algorithm": "SHA1", "digits": "6", "period": "30"} {
		if u.Query().Get(k) != want {
			t.Fatalf("incorrect provisioning field %s", k)
		}
	}
	encoded, err := enrollmentQR(secret, account)
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(encoded, prefix) {
		t.Fatal("QR must be an inline PNG, not an external URL")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, prefix))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 280 || cfg.Height != 280 {
		t.Fatal("unexpected QR dimensions")
	}
}
