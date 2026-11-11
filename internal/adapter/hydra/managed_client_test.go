package hydra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProfileReadProjectsNonsecretFieldsAndRejectsInvalidWholeResponses(t *testing.T) {
	const private = "private-protocol-material-never-returned"
	profile := ManagedPublicClient("controlled-client", "Desktop", "https://app.example.invalid/callback", "tenant", "application")
	encoded, err := json.Marshal(struct {
		PublicClientProfile
		Secret string `json:"client_secret"`
		Token  string `json:"registration_access_token"`
	}{profile, private, private})
	if err != nil {
		t.Fatal("profile fixture failed")
	}
	for _, tt := range []struct {
		name, body string
		status     int
		found, ok  bool
	}{
		{"reviewed projection", string(encoded), 200, true, true},
		{"absent", private, 404, false, true},
		{"unavailable", private, 503, false, false},
		{"truncated", `{"client_secret":"` + private, 200, false, false},
		{"second document", string(encoded) + ` {"secret":"` + private + `"}`, 200, false, false},
		{"trailing junk", string(encoded) + " invalid", 200, false, false},
		{"oversize after valid prefix", string(encoded) + strings.Repeat(" ", 1<<20), 200, false, false},
		{"invalid field type", `{"client_id":42,"client_secret":"` + private + `"}`, 200, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/admin/clients/controlled-client" {
					w.WriteHeader(400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()
			client := New()
			client.Base = server.URL
			actual, found, err := client.ReadPublicClient(context.Background(), profile.ID)
			if found != tt.found || (err == nil) != tt.ok {
				t.Fatal("whole-response boundary failed")
			}
			if err != nil && strings.Contains(err.Error(), private) {
				t.Fatal("upstream private body escaped in error")
			}
			data, _ := json.Marshal(actual)
			if strings.Contains(string(data), private) || (!tt.ok && actual.ID != "") {
				t.Fatal("upstream private or partial profile escaped")
			}
			if found && !profile.Matches(actual) {
				t.Fatal("reviewed nonsecret profile did not survive projection")
			}
		})
	}
}
