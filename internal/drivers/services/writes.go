package services

// Write calls, used only by the outbound layer:
//
//	Kimai        POST /api/timesheets, PATCH|DELETE /api/timesheets/{id},
//	             PATCH /api/timesheets/{id}/stop, PATCH …/export
//	InvoiceNinja POST /api/v1/invoices|payments|expenses|vendors,
//	             POST /api/v1/bank_transactions/match|bulk, PUT /api/v1/expenses/{id}
//	Paperless    POST /api/documents/post_document/ (multipart),
//	             POST /api/documents/bulk_edit/, PATCH /api/documents/{id}/
//	InvoiceNinja POST /api/v1/recurring_expenses(/bulk), PUT …/{id}
//	Wallos       POST /api/subscriptions/set_subscriptions.php (form)
//	Sure         PATCH /api/v1/transactions/{id}

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"

	"andon/internal/drivers/httpclient"
)

// Send runs one write against /api/<path> of Kimai.
func (a KimaiApi) Send(ctx context.Context, method, path string, body any) (any, error) {
	return sendJSON(ctx, method, a.URL+"/api/"+path, a.headers(), body, httpclient.TLSOf(a.Verify))
}

// Send runs one write against /api/v1/<path> of Dawarich, e.g.
// (POST, "areas", {"name": …, "latitude": …}).
func (a DawarichApi) Send(ctx context.Context, method, path string, body any) (any, error) {
	return sendJSON(ctx, method, a.URL+"/api/v1/"+path, map[string]string{"Authorization": "Bearer " + a.Token, "Accept": "application/json"}, body, httpclient.TLSOf(a.Verify))
}

// Post creates one entity in Invoice Ninja, e.g. ("invoices", {...}).
func (a NinjaApi) Post(ctx context.Context, entity string, body any) (any, error) {
	return sendJSON(ctx, http.MethodPost, a.URL+"/api/v1/"+entity, a.headers(), body, httpclient.TLSOf(a.Verify))
}

// Put changes one entity in Invoice Ninja, e.g. ("expenses", "Kx9", {...});
// fields left out stay as they are.
func (a NinjaApi) Put(ctx context.Context, entity, id string, body any) (any, error) {
	return sendJSON(ctx, http.MethodPut, a.URL+"/api/v1/"+entity+"/"+url.PathEscape(id), a.headers(), body, httpclient.TLSOf(a.Verify))
}

// Send runs one write against /api/<path> of Paperless, e.g.
// (POST, "documents/bulk_edit/", {...}).
func (a PaperlessApi) Send(ctx context.Context, method, path string, body any) (any, error) {
	return sendJSON(ctx, method, joinURL(a.URL, "api/"+path), map[string]string{"Authorization": "Token " + a.Token}, body, httpclient.TLSOf(a.Verify))
}

// Upload sends one file to Paperless' consume endpoint; Paperless answers
// with the id of its consumption task.
func (a PaperlessApi) Upload(ctx context.Context, filename, title string, content []byte) (string, error) {
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	if title != "" {
		if err := form.WriteField("title", title); err != nil {
			return "", err
		}
	}
	part, err := form.CreateFormFile("document", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(content); err != nil {
		return "", err
	}
	if err := form.Close(); err != nil {
		return "", err
	}

	headers := map[string]string{"Authorization": "Token " + a.Token, "Content-Type": form.FormDataContentType()}
	resp, err := httpclient.Request(ctx, http.MethodPost, joinURL(a.URL, "api/documents/post_document/"),
		httpclient.Options{Headers: headers, Body: buf.Bytes(), SkipVerify: !a.Verify})
	if err != nil {
		return "", ApiError{err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return "", ApiError{resp.Status}
	}
	task, _ := io.ReadAll(io.LimitReader(resp.Body, httpclient.MaxBody))
	return string(bytes.Trim(task, "\" \n")), nil
}

// Post runs one write script of Wallos with form fields, e.g.
// ("api/subscriptions/set_subscriptions.php", {"action": {"edit"}, …}).
// Wallos answers 200 with success false when it refuses.
func (a WallosApi) Post(ctx context.Context, path string, form url.Values) (any, error) {
	form.Set("api_key", a.Key)
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}
	resp, err := httpclient.Request(ctx, http.MethodPost, joinURL(a.URL, path),
		httpclient.Options{Headers: headers, Body: []byte(form.Encode()), SkipVerify: !a.Verify})
	if err != nil {
		return nil, ApiError{err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, ApiError{resp.Status}
	}

	var out map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, httpclient.MaxBody)).Decode(&out); err != nil {
		return nil, ApiError{"invalid JSON"}
	}
	if ok, _ := out["success"].(bool); !ok {
		msg, _ := out["message"].(string)
		return nil, ApiError{"wallos: " + msg}
	}
	return out, nil
}

// Send runs one write against /api/v1/<path> of Sure, e.g.
// (PATCH, "transactions/7f3…", {"transaction": {"category_id": …}}).
// The API key needs the read_write scope.
func (a SureApi) Send(ctx context.Context, method, path string, body any) (any, error) {
	return sendJSON(ctx, method, joinURL(a.URL, "api/v1/"+path), map[string]string{"X-Api-Key": a.Key}, body, httpclient.TLSOf(a.Verify))
}
