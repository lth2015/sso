package platformapi

import (
	"net/http"
	"regexp"
	"strconv"

	store "github.com/lth2015/sso/internal/storage/mysql"
)

var userAccessPath = regexp.MustCompile(`^/v1/tenants/[a-f0-9]{32}/applications/[a-f0-9]{32}/user-access$`)
var userAccessTag = regexp.MustCompile(`^"ua-([a-f0-9]{32})-([a-f0-9]{32})-([1-9][0-9]*)"$`)

func userAccessETag(p store.ApplicationUserAccessPolicy) string {
	return `"ua-` + p.Tenant + `-` + p.Application + `-` + p.Version + `"`
}
func userAccessRead(w http.ResponseWriter, r *http.Request) bool {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		problem(w, 400, "invalid_request")
		return false
	}
	return true
}
func userAccessPrecondition(w http.ResponseWriter, r *http.Request, tenant, app store.ID) (int64, bool) {
	values := r.Header.Values("If-Match")
	if len(values) == 0 {
		problem(w, 428, "precondition_required")
		return 0, false
	}
	if len(values) != 1 {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	match := userAccessTag.FindStringSubmatch(values[0])
	if match == nil {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	v, e := strconv.ParseInt(match[3], 10, 64)
	if e != nil {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	if match[1] != tenant.String() || match[2] != app.String() {
		problem(w, 412, "version_conflict")
		return 0, false
	}
	return v, true
}
func mountApplicationUserAccess(mux *http.ServeMux, repo store.Store) {
	const base = "/v1/tenants/{tenant}/applications/{application}/user-access"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		if !userAccessRead(w, r) {
			return
		}
		app, ok := pathID(w, r, "application")
		if !ok {
			return
		}
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		v, e := repo.ForAuthorization(authority(r)).APIApplicationUserAccess(r.Context(), app, n, after)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("ETag", userAccessETag(v.Policy))
		respond(w, 200, v)
	})
	mux.HandleFunc("PUT "+base, func(w http.ResponseWriter, r *http.Request) {
		app, ok := pathID(w, r, "application")
		if !ok {
			return
		}
		var body struct {
			Mode string `json:"mode"`
		}
		if !decode(w, r, &body) {
			return
		}
		version, ok := userAccessPrecondition(w, r, authority(r).Tenant, app)
		if !ok {
			return
		}
		v, e := repo.ForAuthorization(authority(r)).SetApplicationUserAccessMode(r.Context(), app, body.Mode, version)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("ETag", userAccessETag(v))
		respond(w, 200, v)
	})
	const memberPath = base + "/members/{member}"
	mux.HandleFunc("GET "+memberPath, func(w http.ResponseWriter, r *http.Request) {
		if !userAccessRead(w, r) {
			return
		}
		app, ok := pathID(w, r, "application")
		if !ok {
			return
		}
		member, ok := pathID(w, r, "member")
		if !ok {
			return
		}
		v, e := repo.ForAuthorization(authority(r)).APIApplicationUserAccessDecision(r.Context(), app, member)
		if e != nil {
			failure(w, e)
			return
		}
		// A changing decision is an observation, not a cache validator or write tag.
		respond(w, 200, v)
	})
	for _, method := range []string{"PUT", "DELETE"} {
		mux.HandleFunc(method+" "+memberPath, func(w http.ResponseWriter, r *http.Request) {
			app, ok := pathID(w, r, "application")
			if !ok {
				return
			}
			member, ok := pathID(w, r, "member")
			if !ok {
				return
			}
			var body struct{}
			if !decode(w, r, &body) {
				return
			}
			version, ok := userAccessPrecondition(w, r, authority(r).Tenant, app)
			if !ok {
				return
			}
			v, e := repo.ForAuthorization(authority(r)).SetApplicationUserAccessMember(r.Context(), app, member, method == "PUT", version)
			if e != nil {
				failure(w, e)
				return
			}
			w.Header().Set("ETag", userAccessETag(v))
			respond(w, 200, v)
		})
	}
}
