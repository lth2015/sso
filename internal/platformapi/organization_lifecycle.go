package platformapi

import (
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http"
)

func mountOrganizationLifecycle(mux *http.ServeMux, repo store.Store) {
	const base = "/v1/tenants/{tenant}/organizations/{org}"
	mux.HandleFunc("GET "+base+"/members", func(w http.ResponseWriter, r *http.Request) {
		org, ok := pathID(w, r, "org")
		if !ok {
			return
		}
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		items, version, err := repo.ForAuthorization(authority(r)).APIOrganizationMembers(r.Context(), org, n+1, after)
		if err != nil {
			failure(w, err)
			return
		}
		etag(w, "authz", version)
		page(w, items, n, func(v store.MemberSummary) string { return v.ID })
	})
	mux.HandleFunc("POST "+base+"/rename", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &body) {
			return
		}
		org, ok := pathID(w, r, "org")
		if !ok {
			return
		}
		version, ok := precondition(w, r, "org")
		if !ok {
			return
		}
		next, err := repo.ForAuthorization(authority(r)).RenameOrganization(r.Context(), org, body.Name, version)
		if err != nil {
			failure(w, err)
			return
		}
		etag(w, "org", next)
		respond(w, 200, map[string]any{"id": org.String(), "org_version": next})
	})
	mux.HandleFunc("DELETE "+base, func(w http.ResponseWriter, r *http.Request) {
		var body struct{}
		if !decode(w, r, &body) {
			return
		}
		org, ok := pathID(w, r, "org")
		if !ok {
			return
		}
		version, ok := precondition(w, r, "org")
		if !ok {
			return
		}
		next, err := repo.ForAuthorization(authority(r)).DeleteOrganization(r.Context(), org, version)
		if err != nil {
			failure(w, err)
			return
		}
		etag(w, "org", next)
		respond(w, 200, map[string]any{"id": org.String(), "org_version": next})
	})
	mux.HandleFunc("DELETE "+base+"/members/{member}", func(w http.ResponseWriter, r *http.Request) {
		var body struct{}
		if !decode(w, r, &body) {
			return
		}
		org, ok := pathID(w, r, "org")
		if !ok {
			return
		}
		member, ok := pathID(w, r, "member")
		if !ok {
			return
		}
		version, ok := precondition(w, r, "authz")
		if !ok {
			return
		}
		if err := repo.ForAuthorization(authority(r)).UnassignOrganization(r.Context(), member, org, version); err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(204)
	})
}
