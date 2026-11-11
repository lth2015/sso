package platformapi

import (
	"net/http"
	"regexp"
	"strconv"

	store "github.com/lth2015/sso/internal/storage/mysql"
)

var invitationTag = regexp.MustCompile(`^"invite-([a-f0-9]{32})-([a-f0-9]{32})-([1-9][0-9]*)"$`)

func invitationETag(tenant store.ID, id string, revision int64) string {
	return `"invite-` + tenant.String() + `-` + id + `-` + strconv.FormatInt(revision, 10) + `"`
}
func invitationPrecondition(w http.ResponseWriter, r *http.Request, tenant, id store.ID) (int64, bool) {
	values := r.Header.Values("If-Match")
	if len(values) == 0 {
		problem(w, 428, "precondition_required")
		return 0, false
	}
	if len(values) != 1 {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	match := invitationTag.FindStringSubmatch(values[0])
	if match == nil {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	revision, err := strconv.ParseInt(match[3], 10, 64)
	if err != nil {
		problem(w, 400, "invalid_precondition")
		return 0, false
	}
	if match[1] != tenant.String() || match[2] != id.String() {
		problem(w, 412, "version_conflict")
		return 0, false
	}
	return revision, true
}
func mountInvitationLifecycle(mux *http.ServeMux, repo store.Store) {
	const base = "/v1/tenants/{tenant}/invitations/{invitation}"
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
			problem(w, 400, "invalid_request")
			return
		}
		id, ok := pathID(w, r, "invitation")
		if !ok {
			return
		}
		v, err := repo.ForAuthorization(authority(r)).APIInvitation(r.Context(), id)
		if err != nil {
			failure(w, err)
			return
		}
		w.Header().Set("ETag", invitationETag(authority(r).Tenant, v.ID, v.Revision))
		respond(w, 200, v)
	})
	mux.HandleFunc("POST "+base+"/reissue", func(w http.ResponseWriter, r *http.Request) {
		var body struct{}
		if !decode(w, r, &body) {
			return
		}
		id, ok := pathID(w, r, "invitation")
		if !ok {
			return
		}
		revision, ok := invitationPrecondition(w, r, authority(r).Tenant, id)
		if !ok {
			return
		}
		v, err := repo.ForAuthorization(authority(r)).APIReissueInvitation(r.Context(), id, revision)
		if err != nil {
			failure(w, err)
			return
		}
		w.Header().Set("ETag", invitationETag(authority(r).Tenant, v.ID, v.Revision))
		respond(w, 200, v)
	})
}
