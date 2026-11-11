package platformapi

import (
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http"
)

func mountSecurity(mux *http.ServeMux, repo store.Store) {
	mux.HandleFunc("GET /v1/me/security", func(w http.ResponseWriter, r *http.Request) {
		value, err := repo.APISecurity(r.Context(), authority(r))
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, value)
	})
	mux.HandleFunc("POST /v1/sessions/revoke-others", func(w http.ResponseWriter, r *http.Request) {
		var body struct{}
		if !decode(w, r, &body) {
			return
		}
		count, err := repo.APIRevokeOtherSessions(r.Context(), authority(r))
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, map[string]int64{"revoked_sessions": count})
	})
}
