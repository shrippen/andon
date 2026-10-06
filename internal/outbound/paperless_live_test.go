package outbound_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/outbound"
	"andon/internal/testkit/live"
)

// kindDocument is a Paperless test entry in the write log.
const kindDocument = "document"

// Paperless consumes uploads in the background; the test waits this
// long for the document, asking every poll.
const (
	consumeWait = 2 * time.Minute
	consumePoll = 2 * time.Second
)

// Paperless task states, see /api/tasks/: "SUCCESS" up to 2.x,
// "success" since.
const (
	taskDone   = "success"
	taskFailed = "failure"
)

// A text file uploaded, a custom field set on it, then deleted: the calls
// of the receipt inbox.
func TestPaperlessLive(t *testing.T) {
	paperless := live.Target(t, live.Paperless)
	api := services.PaperlessApi{URL: paperless.URL, Token: paperless.Token, Verify: paperless.VerifyTLS}
	ctx := context.Background()
	name := live.Name(t)

	// The name makes the content unique: Paperless refuses duplicates.
	task, err := outbound.PaperlessUpload(ctx, paperless, name+".txt", name, []byte(name+"\n"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	id := paperlessConsumed(t, api, task)
	live.Created(t, live.Paperless, kindDocument, id)
	t.Cleanup(func() { paperlessDelete(t, api, id) })

	field := paperlessTextField(t, api)
	if field == 0 {
		t.Skip("no text custom field to set")
	}
	live.Change(t, live.Paperless, live.Update, kindDocument, id)
	if err := outbound.PaperlessFieldsSet(ctx, paperless, id, map[int64]any{field: name}); err != nil {
		t.Fatalf("set fields: %v", err)
	}

	raw, err := api.Get(ctx, "documents/"+strconv.FormatInt(id, 10)+"/", nil)
	if err != nil {
		t.Fatalf("read document: %v", err)
	}
	doc, _ := raw.(map[string]any)
	list, _ := doc["custom_fields"].([]any)
	for _, f := range list {
		m, _ := f.(map[string]any)
		if num(m["field"]) == field && m["value"] == name {
			return
		}
	}
	t.Errorf("custom fields = %v, want %d = %q", list, field, name)
}

// paperlessConsumed waits for the upload task and returns its document.
func paperlessConsumed(t *testing.T, api services.PaperlessApi, task string) int64 {
	t.Helper()
	for end := time.Now().Add(consumeWait); time.Now().Before(end); time.Sleep(consumePoll) {
		raw, err := api.Get(context.Background(), "tasks/", url.Values{"task_id": {task}})
		if err != nil {
			t.Fatalf("task %s: %v", task, err)
		}
		for _, s := range rows(raw) {
			status, _ := s["status"].(string)
			switch strings.ToLower(status) {
			case taskDone:
				return taskDocument(s)
			case taskFailed:
				t.Fatalf("consume failed: %v %v", s["result"], s["result_data"])
			}
		}
	}
	t.Fatalf("task %s not done after %s", task, consumeWait)
	return 0
}

// taskDocument reads a done task's document: related_document up to
// 2.x, related_document_ids since.
func taskDocument(task map[string]any) int64 {
	if ids, _ := task["related_document_ids"].([]any); len(ids) > 0 {
		return num(ids[0])
	}
	return num(task["related_document"])
}

// paperlessTextField returns the first custom field holding text, or 0.
func paperlessTextField(t *testing.T, api services.PaperlessApi) int64 {
	t.Helper()
	raw, err := api.Get(context.Background(), "custom_fields/", nil)
	if err != nil {
		t.Fatalf("custom fields: %v", err)
	}
	for _, f := range rows(raw) {
		if f["data_type"] == "string" {
			return num(f["id"])
		}
	}
	return 0
}

// paperlessDelete removes the test's document and empties it from the
// trash (Paperless 2.10+; older versions delete at once).
func paperlessDelete(t *testing.T, api services.PaperlessApi, id int64) {
	ctx := context.Background()
	live.Change(t, live.Paperless, live.Delete, kindDocument, id)
	if _, err := api.Send(ctx, http.MethodDelete, "documents/"+strconv.FormatInt(id, 10)+"/", nil); err != nil {
		t.Errorf("delete document %d: %v", id, err)
		return
	}

	live.Change(t, live.Paperless, live.Delete, kindDocument, id)
	api.Send(ctx, http.MethodPost, "trash/", map[string]any{"documents": []int64{id}, "action": "empty"})
}
