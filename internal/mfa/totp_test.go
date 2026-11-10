package mfa

import (
	"encoding/base32"
	"testing"
)

func TestRFC6238SHA1VectorsAndReplayWindow(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for _, v := range []struct {
		time int64
		code string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		code, err := Code(secret, v.time/30)
		if err != nil || code != v.code {
			t.Fatal("RFC vector mismatch")
		}
		step, ok := Match(secret, code, v.time, -1)
		if !ok || step != v.time/30 {
			t.Fatal("TOTP rejected")
		}
		if _, ok = Match(secret, code, v.time, step); ok {
			t.Fatal("TOTP replay accepted")
		}
	}
	if _, ok := Match(secret, "nototp", 59, -1); ok {
		t.Fatal("malformed TOTP accepted")
	}
}
