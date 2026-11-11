package platformapi

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/lth2015/sso/internal/adapter/hydra"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

var applicationTag = regexp.MustCompile(`^"app-([a-f0-9]{32})-([a-f0-9]{32})-([1-9][0-9]*)"$`)
var clientCursor = regexp.MustCompile(`^[\x21-\x7e]{1,128}$`)
var clientListPath = regexp.MustCompile(`^/v1/tenants/[a-f0-9]{32}/applications/[a-f0-9]{32}/clients$`)

func applicationETag(tenant store.ID, v store.ApplicationSummary) string {
	return `"app-` + tenant.String() + `-` + v.ID + `-` + strconv.FormatInt(v.Version, 10) + `"`
}
func applicationPrecondition(w http.ResponseWriter, r *http.Request, tenant, app store.ID) (int64, bool) {
	values := r.Header.Values("If-Match")
	if len(values) == 0 {
		problem(w, 428, "precondition_required")
		return 0, false
	}
	if len(values) != 1 {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	match := applicationTag.FindStringSubmatch(values[0])
	if match == nil {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	version, err := strconv.ParseInt(match[3], 10, 64)
	if err != nil {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	if match[1] != tenant.String() || match[2] != app.String() {
		problem(w, 412, "version_conflict")
		return 0, false
	}
	return version, true
}
func clientPagination(w http.ResponseWriter, r *http.Request) (int, string, bool) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		problem(w, 400, "invalid_request")
		return 0, "", false
	}
	for key, value := range q {
		if (key != "limit" && key != "after") || len(value) != 1 || value[0] == "" {
			problem(w, 400, "invalid_request")
			return 0, "", false
		}
	}
	n := 50
	if value := q.Get("limit"); value != "" {
		n, err = strconv.Atoi(value)
		if err != nil || n < 1 || n > 100 {
			problem(w, 400, "invalid_request")
			return 0, "", false
		}
	}
	after := q.Get("after")
	if after != "" && !clientCursor.MatchString(after) {
		problem(w, 400, "invalid_request")
		return 0, "", false
	}
	return n, after, true
}
func mountApplications(mux *http.ServeMux, repo store.Store) {
	const base = "/v1/tenants/{tenant}/applications"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		items, err := repo.ForAuthorization(authority(r)).APIApplications(r.Context(), n+1, after)
		if err != nil {
			failure(w, err)
			return
		}
		page(w, items, n, func(v store.ApplicationSummary) string { return v.ID })
	})
	mux.HandleFunc("GET "+base+"/{application}", func(w http.ResponseWriter, r *http.Request) {
		app, ok := pathID(w, r, "application")
		if !ok {
			return
		}
		v, err := repo.ForAuthorization(authority(r)).APIApplication(r.Context(), app)
		if err != nil {
			failure(w, err)
			return
		}
		w.Header().Set("ETag", applicationETag(authority(r).Tenant, v))
		respond(w, 200, v)
	})
	mux.HandleFunc("GET "+base+"/{application}/clients", func(w http.ResponseWriter, r *http.Request) {
		app, ok := pathID(w, r, "application")
		if !ok {
			return
		}
		n, after, ok := clientPagination(w, r)
		if !ok {
			return
		}
		items, err := repo.ForAuthorization(authority(r)).APIApplicationClients(r.Context(), app, n+1, after)
		if err != nil {
			failure(w, err)
			return
		}
		var next *string
		if len(items) > n {
			items = items[:n]
			value := items[len(items)-1].Client
			next = &value
		}
		respond(w, 200, map[string]any{"issuer": hydra.Issuer, "items": items, "next_cursor": next})
	})
	for _, enabled := range []bool{false, true} {
		action := "disable"
		if enabled {
			action = "enable"
		}
		mux.HandleFunc("POST "+base+"/{application}/"+action, func(w http.ResponseWriter, r *http.Request) {
			app, ok := pathID(w, r, "application")
			if !ok {
				return
			}
			var body struct{}
			if !decode(w, r, &body) {
				return
			}
			version, ok := applicationPrecondition(w, r, authority(r).Tenant, app)
			if !ok {
				return
			}
			v, err := repo.ForAuthorization(authority(r)).SetApplicationEnabled(r.Context(), app, enabled, version)
			if err != nil {
				failure(w, err)
				return
			}
			w.Header().Set("ETag", applicationETag(authority(r).Tenant, v))
			respond(w, 200, v)
		})
	}
}
