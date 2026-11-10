// Package identity coordinates account proof, password hashing and local delivery.
// Password verification is internal and never exchanges passwords for OAuth tokens.
package identity

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lth2015/sso/internal/password"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

var ErrBusy = errors.New("capacity_unavailable")

type Service struct {
	Store store.Store
	aead  cipher.AEAD
	pool  chan struct{}
	dummy string
}
type Message struct {
	ID        string `json:"id"`
	Recipient string `json:"recipient"`
	Purpose   string `json:"purpose"`
	Token     string `json:"token"`
	Expires   int64  `json:"expires_at"`
}

func New(s store.Store, key []byte) (*Service, error) {
	aead, err := NewCipher(key)
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return nil, err
	}
	dummy, err := password.Hash(base64.RawURLEncoding.EncodeToString(secret))
	if err != nil {
		return nil, err
	}
	return &Service{Store: s, aead: aead, pool: make(chan struct{}, 2), dummy: dummy}, nil
}
func NewCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("mail key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func aad(id, recipient, purpose string) []byte {
	return []byte(id + "\x00" + recipient + "\x00" + purpose)
}
func (s *Service) Request(ctx context.Context, email, purpose string) error {
	normalized, err := store.NormalizeEmail(email)
	if err != nil {
		return err
	}
	if purpose != "register" && purpose != "reset" {
		return store.ErrInvalid
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	id, err := store.NewID()
	if err != nil {
		return err
	}
	notification, err := store.NewID()
	if err != nil {
		return err
	}
	expires := time.Now().Add(24 * time.Hour).Unix()
	if purpose == "reset" {
		expires = time.Now().Add(15 * time.Minute).Unix()
	}
	message := Message{notification.String(), normalized, purpose, token, expires}
	plain, err := json.Marshal(message)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := s.aead.Seal(nonce, nonce, plain, aad(notification.String(), normalized, purpose))
	digest := sha256.Sum256([]byte(token))
	return s.Store.EnqueueAccountChallenge(ctx, store.AccountChallenge{ID: id, NotificationID: notification, Digest: digest[:], Purpose: purpose, Email: normalized, Original: strings.TrimSpace(email), Expires: expires, Ciphertext: ciphertext})
}
func tokenDigest(token string) ([]byte, error) {
	if len(token) != 43 {
		return nil, store.ErrChallenge
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return nil, store.ErrChallenge
	}
	digest := sha256.Sum256([]byte(token))
	return digest[:], nil
}
func (s *Service) Complete(ctx context.Context, token, newPassword, purpose string) error {
	if purpose != "register" && purpose != "reset" {
		return store.ErrInvalid
	}
	digest, err := tokenDigest(token)
	if err != nil {
		return err
	}
	if !password.Valid(newPassword) {
		return password.ErrPolicy
	}
	if err = s.Store.ChallengeUsable(ctx, digest, purpose, time.Now().Unix()); err != nil {
		return err
	}
	select {
	case s.pool <- struct{}{}:
		defer func() { <-s.pool }()
	default:
		return ErrBusy
	}
	hash, err := password.Hash(newPassword)
	if err != nil {
		return err
	}
	if purpose == "register" {
		_, err = s.Store.CompleteRegistration(ctx, digest, hash, time.Now().Unix())
		return err
	}
	return s.Store.CompletePasswordReset(ctx, digest, hash, time.Now().Unix())
}

// CheckPassword is only an identity factor. Callers must still bind a trusted
// protocol interaction, enforce MFA and recheck authority before accepting it.
func (s *Service) CheckPassword(ctx context.Context, email, value string) (store.ID, error) {
	select {
	case s.pool <- struct{}{}:
		defer func() { <-s.pool }()
	default:
		return store.ID{}, ErrBusy
	}
	normalized, err := store.NormalizeEmail(email)
	var credential store.PasswordCredential
	if err == nil {
		credential, err = s.Store.PasswordCredential(ctx, normalized)
	}
	encoded := s.dummy
	if err == nil {
		encoded = credential.Hash
	}
	valid := password.Verify(encoded, value)
	if err != nil && !errors.Is(err, store.ErrDenied) && !errors.Is(err, store.ErrInvalid) {
		return store.ID{}, err
	}
	if err != nil || !valid {
		return store.ID{}, store.ErrDenied
	}
	if err = s.Store.CredentialCurrent(ctx, credential.User, credential.Version); err != nil {
		return store.ID{}, err
	}
	return credential.User, nil
}
func Decrypt(aead cipher.AEAD, n store.Notification) ([]byte, error) {
	if len(n.Ciphertext) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("invalid notification")
	}
	plain, err := aead.Open(nil, n.Ciphertext[:aead.NonceSize()], n.Ciphertext[aead.NonceSize():], aad(n.ID.String(), n.Recipient, n.Purpose))
	if err != nil {
		return nil, errors.New("invalid notification")
	}
	var m Message
	if json.Unmarshal(plain, &m) != nil || m.ID != n.ID.String() || m.Recipient != n.Recipient || m.Purpose != n.Purpose || m.Expires != n.Expires {
		return nil, errors.New("notification metadata mismatch")
	}
	if _, err = tokenDigest(m.Token); err != nil {
		return nil, errors.New("invalid notification")
	}
	return plain, nil
}
