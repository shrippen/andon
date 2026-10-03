package outbound

// Tandoor's shopping list: an entry bought is checked off.

import (
	"context"
	"strconv"

	"andon/internal/drivers/httpclient"
	"andon/internal/drivers/services"
)

// TandoorCheck checks one shopping list entry off.
func TandoorCheck(ctx context.Context, to Target, id int64) error {
	api := services.BearerApi(to.URL, to.Token, httpclient.TLSOf(to.VerifyTLS))
	_, err := api.Patch(ctx, "api/shopping-list-entry/"+strconv.FormatInt(id, 10)+"/", map[string]any{"checked": true})
	return err
}
