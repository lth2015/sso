package platformapi

import (
	"net/http"
	"regexp"
	"strconv"

	"github.com/lth2015/sso/internal/adapter/hydra"
	store "github.com/lth2015/sso/internal/storage/mysql"
)

var managedClientTag = regexp.MustCompile(`^"client-([a-f0-9]{32})-([a-f0-9]{32})-([1-9][0-9]*)"$`)

func managedClientETag(tenant store.ID, v store.ManagedClient) string {
	return `"client-` + tenant.String() + `-` + v.ID + `-` + strconv.FormatInt(v.Version, 10) + `"`
}
func managedClientPrecondition(w http.ResponseWriter, r *http.Request, tenant, client store.ID) (int64, bool) {
	values := r.Header.Values("If-Match")
	if len(values) == 0 {
		problem(w, 428, "precondition_required")
		return 0, false
	}
	if len(values) != 1 {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	match := managedClientTag.FindStringSubmatch(values[0])
	if match == nil {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	version, e := strconv.ParseInt(match[3], 10, 64)
	if e != nil {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	if match[1] != tenant.String() || match[2] != client.String() {
		problem(w, 412, "version_conflict")
		return 0, false
	}
	return version, true
}
func mountManagedClients(mux *http.ServeMux, repo store.Store, admin *hydra.Client) {
	const apps = "/v1/tenants/{tenant}/applications"
	const clients = apps + "/{application}/clients"
	mux.HandleFunc("POST "+apps, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &body) {
			return
		}
		v, e := repo.ForAuthorization(authority(r)).APICreateApplication(r.Context(), body.Name)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("ETag", applicationETag(authority(r).Tenant, v))
		w.Header().Set("Location", "/v1/tenants/"+authority(r).Tenant.String()+"/applications/"+v.ID)
		respond(w, 201, v)
	})
	mux.HandleFunc("POST "+clients, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name     string `json:"name"`
			Redirect string `json:"redirect_uri"`
		}
		if !decode(w, r, &body) {
			return
		}
		app, ok := pathID(w, r, "application")
		if !ok {
			return
		}
		v, e := repo.ForAuthorization(authority(r)).APICreateManagedClient(r.Context(), app, body.Name, body.Redirect)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("ETag", managedClientETag(authority(r).Tenant, v))
		w.Header().Set("Location", r.URL.Path+"/"+v.ID)
		respond(w, 201, v)
	})
	mux.HandleFunc("GET "+clients+"/{client}", func(w http.ResponseWriter, r *http.Request) {
		app, ok := pathID(w, r, "application")
		if !ok {
			return
		}
		client, ok := pathID(w, r, "client")
		if !ok {
			return
		}
		v, e := repo.ForAuthorization(authority(r)).APIManagedClient(r.Context(), app, client)
		if e != nil {
			failure(w, e)
			return
		}
		w.Header().Set("ETag", managedClientETag(authority(r).Tenant, v))
		respond(w, 200, v)
	})
	for _, action := range []string{"revoke", "reconcile"} {
		mux.HandleFunc("POST "+clients+"/{client}/"+action, func(w http.ResponseWriter, r *http.Request) {
			var body struct{}
			if !decode(w, r, &body) {
				return
			}
			app, ok := pathID(w, r, "application")
			if !ok {
				return
			}
			client, ok := pathID(w, r, "client")
			if !ok {
				return
			}
			version, ok := managedClientPrecondition(w, r, authority(r).Tenant, client)
			if !ok {
				return
			}
			tenant := repo.ForAuthorization(authority(r))
			if action == "revoke" {
				v, e := tenant.RevokeManagedClient(r.Context(), app, client, version)
				if e != nil {
					failure(w, e)
					return
				}
				w.Header().Set("ETag", managedClientETag(authority(r).Tenant, v))
				respond(w, 200, v)
				return
			}
			v, observation, e := tenant.ReconcileManagedClient(r.Context(), app, client, version, admin)
			if e != nil {
				failure(w, e)
				return
			}
			w.Header().Set("ETag", managedClientETag(authority(r).Tenant, v))
			respond(w, 200, map[string]any{"client": v, "protocol_observation": observation})
		})
	}
}
