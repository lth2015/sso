package hydra

// TokenHookRequest contains only fields consumed by the business hook. Omitted
// protocol payload fields (secrets, codes, refresh tokens) are never retained.
type TokenHookRequest struct {
	Session struct {
		IDToken struct {
			Subject string `json:"subject"`
			Claims  struct {
				Extra map[string]any `json:"ext"`
			} `json:"id_token_claims"`
		} `json:"id_token"`
		Extra            map[string]any `json:"extra"`
		ConsentChallenge string         `json:"consent_challenge"`
	} `json:"session"`
	Request struct {
		ClientID        string   `json:"client_id"`
		GrantTypes      []string `json:"grant_types"`
		RequestedScopes []string `json:"requested_scopes"`
		GrantedScopes   []string `json:"granted_scopes"`
		GrantedAudience []string `json:"granted_audience"`
		Payload         struct {
			Scopes    []string `json:"scope"`
			Audiences []string `json:"audience"`
		} `json:"payload"`
	} `json:"request"`
}
