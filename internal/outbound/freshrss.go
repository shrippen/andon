package outbound

// Writes of the feed suggestions (services/feeds): subscribe a release
// feed, unsubscribe one. Target.Token is "user:apipassword".
//
//	POST subscription/edit  ac=subscribe   s=feed/<url>  t=<title>  a=user/-/label/<category>
//	POST subscription/edit  ac=unsubscribe s=feed/<id>

import (
	"context"
	"net/url"

	"andon/internal/drivers/services"
)

func freshrss(ctx context.Context, to Target) (services.FreshRSSApi, map[string]string, error) {
	api := services.FreshRSSApi{URL: to.URL, Secret: to.Token, Verify: to.VerifyTLS}
	auth, err := api.Open(ctx)
	return api, auth, err
}

// FreshSubscribe subscribes the feed at feedURL, in category when given.
func FreshSubscribe(ctx context.Context, to Target, feedURL, title, category string) error {
	api, auth, err := freshrss(ctx, to)
	if err != nil {
		return err
	}
	form := url.Values{"ac": {"subscribe"}, "s": {"feed/" + feedURL}}
	if title != "" {
		form.Set("t", title)
	}
	if category != "" {
		form.Set("a", "user/-/label/"+category)
	}
	return api.Edit(ctx, auth, "subscription/edit", form)
}

// FreshUnsubscribe removes a subscription by its stream id ("feed/150").
func FreshUnsubscribe(ctx context.Context, to Target, feedID string) error {
	api, auth, err := freshrss(ctx, to)
	if err != nil {
		return err
	}
	return api.Edit(ctx, auth, "subscription/edit", url.Values{"ac": {"unsubscribe"}, "s": {feedID}})
}
