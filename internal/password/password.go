// Package password pins the password hashing format and bounds its resource cost.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

var ErrPolicy = errors.New("password_policy")

const prefix = "$argon2id$v=19$m=65536,t=3,p=1$"

func Valid(value string) bool {
	return utf8.ValidString(value) && len(value) <= 512 && utf8.RuneCountInString(value) >= 12 && utf8.RuneCountInString(value) <= 128
}
func Hash(value string) (string, error) {
	if !Valid(value) {
		return "", ErrPolicy
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(value), salt, 3, 64*1024, 1, 32)
	return prefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash), nil
}
func Verify(encoded, value string) bool {
	if len(value) > 512 || !strings.HasPrefix(encoded, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(encoded, prefix), "$")
	if len(parts) != 2 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil || len(salt) != 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(value), salt, 3, 64*1024, 1, 32)
	return subtle.ConstantTimeCompare(expected, actual) == 1
}
