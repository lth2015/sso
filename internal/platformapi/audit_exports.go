package platformapi

import (
	"net/http"
	"net/url"
	"regexp"

	store "github.com/lth2015/sso/internal/storage/mysql"
)

var auditExportPath = regexp.MustCompile(`^/v1/tenants/[a-f0-9]{32}/audit-exports$`)

func auditExportPagination(w http.ResponseWriter, r *http.Request) (kind string, n int, after store.ID, ok bool) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(q["kind"]) > 1 {
		problem(w, 400, "invalid_request")
		return
	}
	kind = "management"
	if values, exists := q["kind"]; exists {
		if len(values) != 1 || (values[0] != "management" && values[0] != "machine") {
			problem(w, 400, "invalid_request")
			return
		}
		kind = values[0]
	}
	if values, exists := q["after"]; exists && len(values) == 1 && values[0] == "" {
		problem(w, 400, "invalid_request")
		return
	}
	q.Del("kind")
	copyRequest := r.Clone(r.Context())
	copyURL := *r.URL
	copyURL.RawQuery = q.Encode()
	copyRequest.URL = &copyURL
	n, after, ok = pagination(w, copyRequest)
	if ok && q.Get("after") != "" && after == (store.ID{}) {
		problem(w, 404, "not_found")
		ok = false
	}
	return
}
func mountAuditExports(mux *http.ServeMux, repo store.Store) {
	mux.HandleFunc("GET /v1/tenants/{tenant}/audit-exports", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
			problem(w, 400, "invalid_request")
			return
		}
		kind, n, after, ok := auditExportPagination(w, r)
		if !ok {
			return
		}
		scoped := repo.ForAuthorization(authority(r))
		if kind == "management" {
			batch, err := scoped.APIExportManagementAudit(r.Context(), n, after)
			if err != nil {
				failure(w, err)
				return
			}
			respond(w, 200, batch)
			return
		}
		batch, err := scoped.APIExportMachineAudit(r.Context(), n, after)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, batch)
	})
}
