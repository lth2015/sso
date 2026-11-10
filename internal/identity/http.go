package identity

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lth2015/sso/internal/password"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

type bucket struct {
	count int
	until time.Time
}

// Limiter is bounded local protection, not a distributed production rate limit.
type Limiter struct {
	mu      sync.Mutex
	entries map[string]bucket
}

func NewLimiter() *Limiter { return &Limiter{entries: map[string]bucket{}} }
func (l *Limiter) Allow(key string, limit int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.entries[key]
	if ok && now.Before(b.until) {
		if b.count >= limit {
			return false
		}
		b.count++
		l.entries[key] = b
		return true
	}
	if len(l.entries) >= 10000 {
		for k, v := range l.entries {
			if !now.Before(v.until) {
				delete(l.entries, k)
			}
		}
		if len(l.entries) >= 10000 {
			return false
		}
	}
	l.entries[key] = bucket{1, now.Add(window)}
	return true
}
func Handler(service *Service, origin string, assets http.Handler) http.Handler {
	mux := http.NewServeMux()
	limit := NewLimiter()
	capacity := make(chan struct{}, 32)
	mux.Handle("GET /account", assets)
	mux.Handle("GET /account/", assets)
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		// A cheap authoritative DB read; no credentials are exposed.
		if err := service.Store.AccountReady(r.Context()); err != nil {
			writeError(w, 503, "not_ready")
			return
		}
		w.WriteHeader(204)
	})
	for path, purpose := range map[string]string{"/v1/registrations": "register", "/v1/password-resets": "reset"} {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Email string `json:"email"`
			}
			if !decode(w, r, &input) {
				return
			}
			normalized, err := store.NormalizeEmail(input.Email)
			if err != nil {
				writeError(w, 400, "invalid_request")
				return
			}
			// Limit on the normalized alias without storing it as plaintext in this map.
			key := emailLimitKey(normalized)
			if !limit.Allow(key, 5, time.Hour) {
				w.Header().Set("Retry-After", "3600")
				writeError(w, 429, "rate_limited")
				return
			}
			if err = service.Request(r.Context(), input.Email, purpose); err != nil {
				writeError(w, 503, "temporarily_unavailable")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(202)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "若地址符合条件，将发送后续操作说明。"})
		})
	}
	for path, purpose := range map[string]string{"/v1/email-verifications": "register", "/v1/password-resets/complete": "reset"} {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Token    string `json:"token"`
				Password string `json:"password"`
			}
			if !decode(w, r, &input) {
				return
			}
			err := service.Complete(r.Context(), input.Token, input.Password, purpose)
			switch {
			case err == nil:
				w.WriteHeader(204)
			case errors.Is(err, password.ErrPolicy):
				writeError(w, 400, "password_policy")
			case errors.Is(err, store.ErrInvalid):
				writeError(w, 400, "invalid_challenge")
			default:
				writeError(w, 503, "temporarily_unavailable")
			}
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := make([]byte, 12)
		if _, err := rand.Read(id); err != nil {
			writeError(w, 503, "temporarily_unavailable")
			return
		}
		w.Header().Set("X-Request-ID", hex.EncodeToString(id))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		if r.URL.RawQuery != "" {
			writeError(w, 400, "invalid_request")
			return
		}
		if r.Method == http.MethodPost {
			if r.Header.Get("Origin") != origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeError(w, 403, "invalid_origin")
				return
			}
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				writeError(w, 400, "invalid_request")
				return
			}
			if !limit.Allow("ip:"+host, 120, 10*time.Minute) {
				w.Header().Set("Retry-After", "600")
				writeError(w, 429, "rate_limited")
				return
			}
			kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || kind != "application/json" {
				writeError(w, 415, "unsupported_media_type")
				return
			}
		}
		select {
		case capacity <- struct{}{}:
			defer func() { <-capacity }()
		default:
			writeError(w, 503, "temporarily_unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 7*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	body, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(body) {
		writeError(w, 400, "invalid_request")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		writeError(w, 400, "invalid_request")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeError(w, 400, "invalid_request")
		return false
	}
	return true
}
func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "request_id": w.Header().Get("X-Request-ID")})
}
func emailLimitKey(email string) string { return "email:" + hashAlias(strings.ToLower(email)) }
