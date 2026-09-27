package outbound

import (
	"context"
	"net/http"
	"strconv"

	"andon/internal/drivers/services"
)

// NinjaExpenseSet writes custom values of one expense (hashed v5 id), e.g.
// {"custom_value2": "https://docs…/documents/44/"}; other fields stay.
func NinjaExpenseSet(ctx context.Context, to Target, key string, values map[string]string) error {
	_, err := services.NinjaApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}.Put(ctx, "expenses", key, values)
	return err
}

// PaperlessFieldsSet writes custom field values of one document; fields
// left out stay. bulk_edit adds missing fields to the document; versions
// without it get the whole list back by PATCH.
func PaperlessFieldsSet(ctx context.Context, to Target, docID int64, values map[int64]any) error {
	api := services.PaperlessApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	add := make(map[string]any, len(values))
	for id, v := range values {
		add[strconv.FormatInt(id, 10)] = v
	}
	_, err := api.Send(ctx, http.MethodPost, "documents/bulk_edit/", map[string]any{
		"documents": []int64{docID}, "method": "modify_custom_fields",
		"parameters": map[string]any{"add_custom_fields": add, "remove_custom_fields": []int64{}},
	})
	if err == nil {
		return nil
	}

	path := "documents/" + strconv.FormatInt(docID, 10) + "/"
	current, getErr := api.Get(ctx, path, nil)
	if getErr != nil {
		return err
	}
	merged := map[int64]any{}
	doc, _ := current.(map[string]any)
	list, _ := doc["custom_fields"].([]any)
	for _, raw := range list {
		f, _ := raw.(map[string]any)
		if id, ok := f["field"].(float64); ok {
			merged[int64(id)] = f["value"]
		}
	}
	for id, v := range values {
		merged[id] = v
	}
	fields := make([]map[string]any, 0, len(merged))
	for id, v := range merged {
		fields = append(fields, map[string]any{"field": id, "value": v})
	}
	_, err = api.Send(ctx, http.MethodPatch, path, map[string]any{"custom_fields": fields})
	return err
}
