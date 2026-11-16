package identity

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	store "github.com/lth2015/sso/internal/storage/mysql"
)

// SMTPConfig requires authenticated submission over verified STARTTLS.
type SMTPConfig struct {
	Host, From, Username, Password, AccountURL string
	Port                                       int
}

func (c SMTPConfig) Validate() error {
	u, err := url.Parse(c.AccountURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/account" {
		return errors.New("invalid mail account URL")
	}
	a, err := mail.ParseAddress(c.From)
	if err != nil || a.Address != c.From || strings.ContainsAny(c.From, "\r\n") || c.Host == "" || strings.ContainsAny(c.Host, "/\r\n") || c.Port < 1 || c.Port > 65535 || c.Username == "" || c.Password == "" {
		return errors.New("invalid SMTP configuration")
	}
	return nil
}

// LOGIN is supported by Exchange Online. Credentials are never offered before TLS.
type smtpLogin struct{ username, password string }

func (a smtpLogin) Start(s *smtp.ServerInfo) (string, []byte, error) {
	if !s.TLS {
		return "", nil, errors.New("SMTP TLS required")
	}
	return "LOGIN", nil, nil
}
func (a smtpLogin) Next(prompt []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(prompt))) {
	case "username:":
		return []byte(a.username), nil
	case "password:":
		return []byte(a.password), nil
	}
	return nil, errors.New("unexpected SMTP authentication challenge")
}

func smtpMessage(c SMTPConfig, n store.Notification, aead cipher.AEAD) ([]byte, error) {
	plain, err := Decrypt(aead, n)
	if err != nil {
		return nil, err
	}
	var m Message
	if err = json.Unmarshal(plain, &m); err != nil {
		return nil, err
	}
	recipient, err := mail.ParseAddress(n.Recipient)
	if err != nil || recipient.Address != n.Recipient || strings.ContainsAny(n.Recipient, "\r\n") {
		return nil, errors.New("invalid notification")
	}
	subject, action := "验证你的邮箱", "验证邮箱并创建账户"
	if m.Purpose == "reset" {
		subject, action = "重置你的账户密码", "重置密码"
	} else if m.Purpose != "register" {
		return nil, errors.New("invalid notification")
	}
	link, _ := url.Parse(c.AccountURL)
	link.Fragment = url.Values{"purpose": {m.Purpose}, "token": {m.Token}}.Encode()
	body := fmt.Sprintf("请打开以下链接%s：\r\n\r\n%s\r\n\r\n有效期至 %s。链接仅可使用一次，请勿转发。\r\n如果不是你发起的操作，请忽略本邮件。\r\n", action, link.String(), time.Unix(m.Expires, 0).UTC().Format(time.RFC3339))
	// A stable ID assists deduplication; SMTP acceptance and DB commit are not atomic.
	host := strings.Split(c.From, "@")[1]
	return []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@%s>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n%s", c.From, n.Recipient, mime.QEncoding.Encode("utf-8", subject), time.Now().UTC().Format(time.RFC1123Z), n.ID.String(), host, encodeMailBody(body))), nil
}

func (c SMTPConfig) Sink(ctx context.Context, aead cipher.AEAD) func(store.Notification) error {
	return func(n store.Notification) error {
		if err := c.Validate(); err != nil {
			return err
		}
		message, err := smtpMessage(c, n, aead)
		if err != nil {
			return err
		}
		return c.withClient(ctx, func(client *smtp.Client) error {
			if err = client.Mail(c.From); err != nil {
				return err
			}
			if err = client.Rcpt(n.Recipient); err != nil {
				return err
			}
			w, err := client.Data()
			if err != nil {
				return err
			}
			if _, err = w.Write(message); err != nil {
				return err
			}
			if err = w.Close(); err != nil {
				return err
			}
			// DATA acceptance is success even if the subsequent QUIT response is lost.
			return nil
		})
	}
}

// Check authenticates over verified TLS without submitting any message.
func (c SMTPConfig) Check(ctx context.Context) error {
	if err := c.Validate(); err != nil {
		return err
	}
	return c.withClient(ctx, func(*smtp.Client) error { return nil })
}

func (c SMTPConfig) withClient(ctx context.Context, use func(*smtp.Client) error) error {
	var err error
	step, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(step, "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := step.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return err
	}
	done := context.AfterFunc(step, func() { conn.Close() })
	defer done()
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); !ok {
		return errors.New("SMTP STARTTLS required")
	}
	if err = client.StartTLS(&tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}); err != nil {
		return err
	}
	ok, methods := client.Extension("AUTH")
	if !ok {
		return errors.New("SMTP authentication required")
	}
	var auth smtp.Auth
	for _, method := range strings.Fields(methods) {
		if method == "LOGIN" {
			auth = smtpLogin{c.Username, c.Password}
			break
		}
		if method == "PLAIN" {
			auth = smtp.PlainAuth("", c.Username, c.Password, c.Host)
		}
	}
	if auth == nil {
		return errors.New("SMTP authentication unsupported")
	}
	if err = client.Auth(auth); err != nil {
		return err
	}

	return use(client)
}

func encodeMailBody(body string) string {
	var b bytes.Buffer
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(body))
	_ = w.Close()
	return b.String()
}
