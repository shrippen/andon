package outbound

// Grocy's shopping list: the products below their minimum stock go on it.

import (
	"context"

	"andon/internal/drivers/httpclient"
	"andon/internal/drivers/services"
)

// GrocyAddMissing puts every product below its minimum stock on the
// default shopping list.
func GrocyAddMissing(ctx context.Context, to Target) error {
	_, err := services.HeaderApi(to.URL, "GROCY-API-KEY", to.Token, httpclient.TLSOf(to.VerifyTLS)).Post(ctx, "api/stock/shoppinglist/add-missing-products", map[string]any{})
	return err
}
