package platformapi

import (
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http"
	"regexp"
)

var rolePolicyHistoryPath = regexp.MustCompile(`^/v1/tenants/[a-f0-9]{32}/roles/[a-f0-9]{32}/policy-history$`)

func mountRolePolicyHistory(mux *http.ServeMux, repo store.Store) {
	mux.HandleFunc("GET /v1/tenants/{tenant}/roles/{role}/policy-history", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			problem(w, 400, "invalid_request")
			return
		}
		for _, key := range []string{"limit", "after"} {
			if values, present := r.URL.Query()[key]; present && (len(values) != 1 || values[0] == "") {
				problem(w, 400, "invalid_request")
				return
			}
		}
		role, ok := pathID(w, r, "role")
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
		page, e := repo.ForAuthorization(authority(r)).APIRolePolicyHistory(r.Context(), role, limit, cursor)
		if e != nil {
			failure(w, e)
			return
		}
		respond(w, 200, page)
	})
}
