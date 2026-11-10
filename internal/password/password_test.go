package password

import (
	"strings"
	"testing"
)

func TestArgon2idSaltFormatAndVerification(t *testing.T) {
	a, err := Hash("A long unique example passphrase!")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Hash("A long unique example passphrase!")
	if err != nil {
		t.Fatal(err)
	}
	if a == b || !strings.HasPrefix(a, prefix) {
		t.Fatal("hash salt/format invalid")
	}
	if !Verify(a, "A long unique example passphrase!") || Verify(a, "Incorrect passphrase value!") {
		t.Fatal("verification mismatch")
	}
	for _, bad := range []string{"$argon2id$v=19$m=99999999,t=3,p=1$x$x", a + "$extra", prefix + "x$x", ""} {
		if Verify(bad, "A long unique example passphrase!") {
			t.Fatal("malformed hash accepted")
		}
	}
}
func TestPasswordPolicyPreservesUnicodeAndWhitespace(t *testing.T) {
	if Valid("short") || Valid(strings.Repeat("x", 129)) || Valid(string([]byte{0xff})) {
		t.Fatal("invalid password policy")
	}
	if !Valid("中文密码需要足够长度和不同字符") || !Valid("   spaces are significant   ") {
		t.Fatal("valid password rejected")
	}
}
