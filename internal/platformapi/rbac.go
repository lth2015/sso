package platformapi

import (
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http"
)

func mountRBAC(mux *http.ServeMux, repo store.Store) {
	mux.HandleFunc("GET /v1/tenants/{tenant}/permissions", func(w http.ResponseWriter, r *http.Request) {
		value, err := repo.ForAuthorization(authority(r)).APIPermissions(r.Context())
		if err != nil {
			failure(w, err)
			return
		}
		etag(w, "authz", value.Version)
		respond(w, 200, value)
	})
	mux.HandleFunc("GET /v1/tenants/{tenant}/roles", func(w http.ResponseWriter, r *http.Request) {
		limit, after, ok := pagination(w, r)
		if !ok {
			return
		}
		items, version, err := repo.ForAuthorization(authority(r)).APIRoles(r.Context(), limit+1, after)
		if err != nil {
			failure(w, err)
			return
		}
		etag(w, "authz", version)
		page(w, items, limit, func(v store.RoleSummary) string { return v.ID })
	})
	mux.HandleFunc("POST /v1/tenants/{tenant}/roles", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Code     string              `json:"code"`
			Policies *[]store.RolePolicy `json:"policies"`
		}
		if !decodeNested(w, r, &input) {
			return
		}
		if input.Policies == nil {
			problem(w, 400, "invalid_request")
			return
		}
		version, ok := precondition(w, r, "authz")
		if !ok {
			return
		}
		id, err := repo.ForAuthorization(authority(r)).APICreateRole(r.Context(), input.Code, *input.Policies, version)
		if err != nil {
			failure(w, err)
			return
		}
		etag(w, "authz", version+1)
		respond(w, 201, map[string]any{"id": id.String(), "authz_version": version + 1})
	})
	mux.HandleFunc("PUT /v1/tenants/{tenant}/roles/{role}", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Policies *[]store.RolePolicy `json:"policies"`
		}
		if !decodeNested(w, r, &input) {
			return
		}
		if input.Policies == nil {
			problem(w, 400, "invalid_request")
			return
		}
		id, ok := pathID(w, r, "role")
		if !ok {
			return
		}
		version, ok := precondition(w, r, "authz")
		if !ok {
			return
		}
		if err := repo.ForAuthorization(authority(r)).APIReplaceRole(r.Context(), id, *input.Policies, version); err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("DELETE /v1/tenants/{tenant}/roles/{role}", func(w http.ResponseWriter, r *http.Request) {
		var body struct{}
		if !decode(w, r, &body) {
			return
		}
		id, ok := pathID(w, r, "role")
		if !ok {
			return
		}
		version, ok := precondition(w, r, "authz")
		if !ok {
			return
		}
		if err := repo.ForAuthorization(authority(r)).APIDeleteRole(r.Context(), id, version); err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /v1/tenants/{tenant}/members/{member}/roles", func(w http.ResponseWriter, r *http.Request) {
		member, ok := pathID(w, r, "member")
		if !ok {
			return
		}
		items, version, err := repo.ForAuthorization(authority(r)).APIMemberRoles(r.Context(), member)
		if err != nil {
			failure(w, err)
			return
		}
		etag(w, "authz", version)
		respond(w, 200, map[string]any{"items": items})
	})
	for _, method := range []string{"PUT", "DELETE"} {
		mux.HandleFunc(method+" /v1/tenants/{tenant}/members/{member}/roles/{role}", func(w http.ResponseWriter, r *http.Request) {
			var body struct{}
			if !decode(w, r, &body) {
				return
			}
			member, ok := pathID(w, r, "member")
			if !ok {
				return
			}
			role, ok := pathID(w, r, "role")
			if !ok {
				return
			}
			version, ok := precondition(w, r, "authz")
			if !ok {
				return
			}
			if err := repo.ForAuthorization(authority(r)).APIBindRole(r.Context(), member, role, version, r.Method == "DELETE"); err != nil {
				failure(w, err)
				return
			}
			w.WriteHeader(204)
		})
	}
}
