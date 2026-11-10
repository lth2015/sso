// Package mfa implements the bounded SHA-1 / 6-digit / 30-second RFC 6238 profile.
package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
)

func NewSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}
func Code(secret string, step int64) (string, error) {
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil || len(raw) != 20 || step < 0 {
		return "", fmt.Errorf("invalid TOTP material")
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, raw)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 15
	value := (binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", value), nil
}
func Match(secret, code string, now, lastStep int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, v := range code {
		if v < '0' || v > '9' {
			return 0, false
		}
	}
	for _, step := range []int64{now / 30, now/30 - 1, now/30 + 1} {
		if step <= lastStep {
			continue
		}
		expected, err := Code(secret, step)
		if err == nil && subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
			return step, true
		}
	}
	return 0, false
}
