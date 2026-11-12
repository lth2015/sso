package platformapi

import (
	"net/http"
	"regexp"

	store "github.com/lth2015/sso/internal/storage/mysql"
)

var applicationAccessHistoryPath = regexp.MustCompile(`^/v1/tenants/[a-f0-9]{32}/applications/[a-f0-9]{32}/user-access/history$`)

func mountApplicationAccessHistory(mux *http.ServeMux, repo store.Store) {
	mux.HandleFunc("GET /v1/tenants/{tenant}/applications/{application}/user-access/history", func(w http.ResponseWriter, r *http.Request) {
		if !userAccessRead(w, r) {
			return
		}
		for _, key := range []string{"limit", "after"} {
			if values, present := r.URL.Query()[key]; present && (len(values) != 1 || values[0] == "") {
				problem(w, 400, "invalid_request")
				return
			}
		}
		app, ok := pathID(w, r, "application")
		if !ok {
			return
		}
		limit, after, ok := pagination(w, r)
		if !ok {
			return
		}
		var cursor *store.ID
		if _, present := r.URL.Query()["after"]; present {
			cursor = &after
		}
		v, e := repo.ForAuthorization(authority(r)).APIApplicationAccessHistory(r.Context(), app, limit, cursor)
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, v)
	})
}
