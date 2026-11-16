package identity

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"mime/quotedprintable"
	"net"
	"strings"
	"testing"
	"time"

	store "github.com/lth2015/sso/internal/storage/mysql"
)

func TestSMTPMessageAndConfiguration(t *testing.T) {
	cfg := SMTPConfig{Host: "smtp.office365.com", Port: 587, From: "sso@example.com", Username: "sso@example.com", Password: "test-only", AccountURL: "https://sso.example.com/account"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"http://sso.example.com/account", "https://sso.example.com/account?redirect=evil", "https://sso.example.com/account#token=evil"} {
		bad := cfg
		bad.AccountURL = address
		if bad.Validate() == nil {
			t.Fatal("unsafe URL accepted")
		}
	}
	aead, _ := NewCipher(make([]byte, 32))
	id, _ := store.NewID()
	for _, purpose := range []string{"register", "reset"} {
		n := store.Notification{ID: id, Recipient: "user@example.com", Purpose: purpose, Expires: time.Now().Add(time.Hour).Unix()}
		token := strings.Repeat("a", 43)
		plain, _ := json.Marshal(Message{n.ID.String(), n.Recipient, n.Purpose, token, n.Expires})
		nonce := make([]byte, aead.NonceSize())
		_, _ = rand.Read(nonce)
		n.Ciphertext = aead.Seal(nonce, nonce, plain, aad(n.ID.String(), n.Recipient, n.Purpose))
		msg, err := smtpMessage(cfg, n, aead)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.SplitN(string(msg), "\r\n\r\n", 2)
		body, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(parts[1])))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "https://sso.example.com/account#purpose="+purpose+"&token="+token) {
			t.Fatal("incorrect verification link")
		}
		n.Ciphertext[0] ^= 1
		if _, err = smtpMessage(cfg, n, aead); err == nil {
			t.Fatal("tampered message accepted")
		}
	}
}

func TestSMTPRefusesPlaintextServerBeforeCredentials(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	finished := make(chan string, 1)
	go func() {
		c, err := listener.Accept()
		if err != nil {
			finished <- ""
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = io.WriteString(c, "220 test ESMTP\r\n")
		buf := make([]byte, 2048)
		n, _ := c.Read(buf)
		conversation := string(buf[:n])
		_, _ = io.WriteString(c, "250-test\r\n250 AUTH LOGIN PLAIN\r\n")
		n, _ = c.Read(buf)
		conversation += string(buf[:n])
		finished <- conversation
	}()
	cfg := SMTPConfig{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, From: "sso@example.com", Username: "private-user", Password: "private-password", AccountURL: "https://sso.example.com/account"}
	aead, _ := NewCipher(make([]byte, 32))
	id, _ := store.NewID()
	n := store.Notification{ID: id, Recipient: "user@example.com", Purpose: "register", Expires: time.Now().Add(time.Hour).Unix()}
	plain, _ := json.Marshal(Message{n.ID.String(), n.Recipient, n.Purpose, strings.Repeat("a", 43), n.Expires})
	nonce := make([]byte, aead.NonceSize())
	n.Ciphertext = aead.Seal(nonce, nonce, plain, aad(n.ID.String(), n.Recipient, n.Purpose))
	err = cfg.Sink(context.Background(), aead)(n)
	if err == nil || err.Error() != "SMTP STARTTLS required" {
		t.Fatalf("expected STARTTLS refusal: %v", err)
	}
	conversation := <-finished
	if strings.Contains(conversation, "AUTH") || strings.Contains(conversation, "private-") {
		t.Fatal("credentials sent before TLS")
	}
}
