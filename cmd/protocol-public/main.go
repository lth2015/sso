// A local network boundary exposing only the protocol paths BFFs need.
package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

func main() {
	target, _ := url.Parse("http://hydra-login:4444")
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{Proxy: nil, ResponseHeaderTimeout: 4 * time.Second}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) { http.Error(w, "protocol_unavailable", 503) }
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed := (r.Method == "GET" && (r.URL.Path == "/.well-known/openid-configuration" || r.URL.Path == "/.well-known/jwks.json")) || (r.Method == "POST" && (r.URL.Path == "/oauth2/token" || r.URL.Path == "/oauth2/revoke"))
		if !allowed || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		proxy.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: ":4444", Handler: handler, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 8 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal("local protocol boundary failed")
	}
}
