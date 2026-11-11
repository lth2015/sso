package platformapi

import (
	"github.com/lth2015/sso/internal/adapter/hydra"
	store "github.com/lth2015/sso/internal/storage/mysql"
	"net/http"
)

func mountProtocolObservations(mux *http.ServeMux, repo store.Store, admin *hydra.Client) {
	const root = "/v1/tenants/{tenant}/applications/{application}"
	mux.HandleFunc("GET "+root+"/clients/{client}/protocol-status", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
			problem(w, 400, "invalid_request")
			return
		}
		app, ok := pathID(w, r, "application")
		if !ok {
			return
		}
		id, ok := pathID(w, r, "client")
		if !ok {
			return
		}
		v, err := repo.ForAuthorization(authority(r)).APIManagedClientProtocol(r.Context(), app, id, admin)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, v)
	})
	mux.HandleFunc("GET "+root+"/service-principals/{principal}/credentials/{credential}/protocol-status", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
			problem(w, 400, "invalid_request")
			return
		}
		app, principal, id, ok := machineIDs(w, r, true)
		if !ok {
			return
		}
		v, err := repo.ForAuthorization(authority(r)).APIMachineCredentialProtocol(r.Context(), app, principal, id, admin)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, 200, v)
	})
}
