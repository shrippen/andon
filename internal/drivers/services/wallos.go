package services

import (
	"context"
	"net/url"

	"andon/internal/drivers/httpclient"
)

// WallosApi reads Wallos' API with the user's API key (Wallos → Profil →
// API-Schlüssel), sent as the api_key parameter.
type WallosApi struct {
	URL    string
	Key    string
	Verify bool
}

// Get calls one API script, e.g. "api/subscriptions/get_subscriptions.php".
func (a WallosApi) Get(ctx context.Context, path string, params url.Values) (any, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("api_key", a.Key)
	return fetchJSON(ctx, joinURL(a.URL, path), map[string]string{"Accept": "application/json"}, params, httpclient.TLSOf(a.Verify))
}
