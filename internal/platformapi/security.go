package platformapi

import (
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http"
)

func mountSecurity(mux *http.ServeMux, repo store.Store) {
	mux.HandleFunc("GET /v1/me/security-events", func(w http.ResponseWriter, r *http.Request) {
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		if r.URL.Query().Has("after") && after == (store.ID{}) {
			problem(w, 404, "not_found")
			return
		}
		items, err := repo.APISecurityEvents(r.Context(), authority(r), n+1, after)
		if err != nil {
			failure(w, err)
			return
		}
		page(w, items, n, func(v store.SecurityEventSummary) string { return v.ID })
	})
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
