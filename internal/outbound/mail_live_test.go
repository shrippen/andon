package outbound_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/outbound"
	"andon/internal/settings"
	"andon/internal/testkit/live"
)

// kindMessage is a message in the mail catcher, in the write log.
const kindMessage = "message"

// mailWait is how long a message may take to reach the catcher.
const mailWait = 10 * time.Second

// A mail as Andon sends digests and invitations, through SMTP into a
// catcher: never to a real inbox (live.Target).
func TestMailLive(t *testing.T) {
	smtp := live.Target(t, live.SMTP)
	box := live.Target(t, live.Mailbox)
	subject := live.Name(t)

	cfg := settings.Settings{SMTPURL: smtp.URL, SMTPFrom: "Andon <andon@example.test>"}
	if err := outbound.SendMail(cfg, outbound.Mail{To: "andon-test@example.test", Subject: subject, Text: "Prüfung ✓", HTML: "<p>Prüfung ✓</p>"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	m := caught(t, box, subject)
	if from, _ := asObject(m["From"])["Address"].(string); from != "andon@example.test" {
		t.Errorf("from = %v", m["From"])
	}
	if s, _ := m["Snippet"].(string); !strings.Contains(s, "Prüfung ✓") {
		t.Errorf("text = %q, want the umlaut and the check mark intact", s)
	}
}

// A push through the Apprise API, as notify sends hints; its channel is
// the catcher's SMTP, so the push arrives there as a mail.
func TestAppriseLive(t *testing.T) {
	apprise := live.Target(t, live.Apprise)
	box := live.Target(t, live.Mailbox)
	title := live.Name(t)

	if err := outbound.Send(context.Background(), apprise.URL, apprise.Token, title, "Hinweis ✓"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if s, _ := caught(t, box, title)["Snippet"].(string); !strings.Contains(s, "Hinweis ✓") {
		t.Errorf("body = %q", s)
	}
}

// caught waits for the message with subject in the catcher, logs it as
// the test's own and deletes it when the test ends.
func caught(t *testing.T, box outbound.Target, subject string) map[string]any {
	t.Helper()
	api := services.KeyedApi{URL: box.URL}
	ctx := context.Background()
	deadline := time.Now().Add(mailWait)
	for {
		raw, err := api.Get(ctx, "api/v1/search", url.Values{"query": {`subject:"` + subject + `"`}})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if list, _ := asObject(raw)["messages"].([]any); len(list) == 1 {
			m := asObject(list[0])
			id, _ := m["ID"].(string)
			live.Created(t, live.Mailbox, kindMessage, id)
			t.Cleanup(func() {
				live.Change(t, live.Mailbox, live.Delete, kindMessage, id)
				req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, box.URL+"/api/v1/messages", strings.NewReader(`{"IDs": ["`+id+`"]}`))
				req.Header.Set("Content-Type", "application/json")
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Errorf("delete: %v", err)
					return
				}
				resp.Body.Close()
			})
			return m
		}
		if time.Now().After(deadline) {
			t.Fatalf("no message %q after %s", subject, mailWait)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
