package platformapi

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/lth2015/sso/internal/adapter/hydra"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

var machineTag = regexp.MustCompile(`^"(sp|cred)-([a-f0-9]{32})-([a-f0-9]{32})-([1-9][0-9]*)"$`)

func machineETag(kind string, tenant store.ID, id string, version int64) string {
	return `"` + kind + `-` + tenant.String() + `-` + id + `-` + strconv.FormatInt(version, 10) + `"`
}
func machinePrecondition(w http.ResponseWriter, r *http.Request, kind string, tenant, id store.ID) (int64, bool) {
	h := r.Header.Values("If-Match")
	if len(h) == 0 {
		problem(w, 428, "precondition_required")
		return 0, false
	}
	if len(h) != 1 {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	m := machineTag.FindStringSubmatch(h[0])
	if m == nil || m[1] != kind {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	v, e := strconv.ParseInt(m[4], 10, 64)
	if e != nil {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	if m[2] != tenant.String() || m[3] != id.String() {
		problem(w, 412, "version_conflict")
		return 0, false
	}
	return v, true
}
func machineIDs(w http.ResponseWriter, r *http.Request, credential bool) (app, principal, key store.ID, ok bool) {
	app, ok = pathID(w, r, "application")
	if !ok {
		return
	}
	principal, ok = pathID(w, r, "principal")
	if !ok {
		return
	}
	if credential {
		key, ok = pathID(w, r, "credential")
	}
	return
}
func mountMachineManagement(mux *http.ServeMux, repo store.Store, admin *hydra.Client) {
	const base = "/v1/tenants/{tenant}/applications/{application}/service-principals"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		app, ok := pathID(w, r, "application")
		if !ok {
			return
		}
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		items, e := repo.ForAuthorization(authority(r)).APIPrincipals(r.Context(), app, n+1, after)
		if e != nil {
			failure(w, e)
			return
		}
		page(w, items, n, func(v store.ServicePrincipalSummary) string { return v.ID })
	})
	mux.HandleFunc("POST "+base, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &body) {
			return
		}
		app, ok := pathID(w, r, "application")
		if !ok {
			return
		}
		v, e := repo.ForAuthorization(authority(r)).APICreatePrincipal(r.Context(), app, body.Name)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("ETag", machineETag("sp", authority(r).Tenant, v.ID, v.Version))
		w.Header().Set("Location", r.URL.Path+"/"+v.ID)
		respond(w, 201, v)
	})
	mux.HandleFunc("GET "+base+"/{principal}", func(w http.ResponseWriter, r *http.Request) {
		app, principal, _, ok := machineIDs(w, r, false)
		if !ok {
			return
		}
		v, e := repo.ForAuthorization(authority(r)).APIPrincipal(r.Context(), app, principal)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("ETag", machineETag("sp", authority(r).Tenant, v.ID, v.Version))
		respond(w, 200, v)
	})
	for _, enabled := range []bool{false, true} {
		action := "disable"
		if enabled {
			action = "enable"
		}
		mux.HandleFunc("POST "+base+"/{principal}/"+action, func(w http.ResponseWriter, r *http.Request) {
			var body struct{}
			if !decode(w, r, &body) {
				return
			}
			app, principal, _, ok := machineIDs(w, r, false)
			if !ok {
				return
			}
			version, ok := machinePrecondition(w, r, "sp", authority(r).Tenant, principal)
			if !ok {
				return
			}
			v, e := repo.ForAuthorization(authority(r)).SetPrincipalEnabled(r.Context(), app, principal, enabled, version)
			if e != nil {
				failure(w, e)
				return
			}
			w.Header().Set("ETag", machineETag("sp", authority(r).Tenant, v.ID, v.Version))
			respond(w, 200, v)
		})
	}
	const keys = base + "/{principal}/credentials"
	mux.HandleFunc("GET "+keys, func(w http.ResponseWriter, r *http.Request) {
		app, principal, _, ok := machineIDs(w, r, false)
		if !ok {
			return
		}
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		items, e := repo.ForAuthorization(authority(r)).APIMachineCredentials(r.Context(), app, principal, n+1, after)
		if e != nil {
			failure(w, e)
			return
		}
		page(w, items, n, func(v store.MachineCredentialSummary) string { return v.ID })
	})
	mux.HandleFunc("POST "+keys, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
			Days int    `json:"expires_in_days"`
		}
		if !decode(w, r, &body) {
			return
		}
		app, principal, _, ok := machineIDs(w, r, false)
		if !ok {
			return
		}
		result, e := repo.ForAuthorization(authority(r)).CreateMachineCredential(r.Context(), app, principal, body.Name, body.Days, admin)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("ETag", machineETag("cred", authority(r).Tenant, result.Credential.ID, result.Credential.Version))
		w.Header().Set("Location", r.URL.Path+"/"+result.Credential.ID)
		respond(w, 201, result)
	})
	mux.HandleFunc("GET "+keys+"/{credential}", func(w http.ResponseWriter, r *http.Request) {
		app, principal, key, ok := machineIDs(w, r, true)
		if !ok {
			return
		}
		v, e := repo.ForAuthorization(authority(r)).APIMachineCredential(r.Context(), app, principal, key)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("ETag", machineETag("cred", authority(r).Tenant, v.ID, v.Version))
		respond(w, 200, v)
	})
	for _, action := range []string{"revoke", "cleanup"} {
		mux.HandleFunc("POST "+keys+"/{credential}/"+action, func(w http.ResponseWriter, r *http.Request) {
			var body struct{}
			if !decode(w, r, &body) {
				return
			}
			app, principal, key, ok := machineIDs(w, r, true)
			if !ok {
				return
			}
			version, ok := machinePrecondition(w, r, "cred", authority(r).Tenant, key)
			if !ok {
				return
			}
			var v store.MachineCredentialSummary
			var e error
			if action == "revoke" {
				v, e = repo.ForAuthorization(authority(r)).RevokeMachineCredential(r.Context(), app, principal, key, version)
			} else {
				v, e = repo.ForAuthorization(authority(r)).CleanupMachineCredential(r.Context(), app, principal, key, version, admin)
			}
			if e != nil {
				failure(w, e)
				return
			}
			w.Header().Set("ETag", machineETag("cred", authority(r).Tenant, v.ID, v.Version))
			respond(w, 200, v)
		})
	}
	mux.HandleFunc("GET /v1/tenants/{tenant}/machine-events", func(w http.ResponseWriter, r *http.Request) {
		n, after, ok := pagination(w, r)
		if !ok {
			return
		}
		items, e := repo.ForAuthorization(authority(r)).APIMachineEvents(r.Context(), n+1, after)
		if e != nil {
			failure(w, e)
			return
		}
		page(w, items, n, func(v store.MachineEventSummary) string { return v.ID })
	})
}
