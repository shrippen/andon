package web

import (
	"andon/internal/services/widgetlib"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"andon/internal/enums"
)

// TestWidgetsForStarter: a new Healthchecks connection is offered the
// heartbeats tile although the tile reads its connection as a peer.
func TestWidgetsForStarter(t *testing.T) {
	for _, kind := range widgetsFor(enums.ServiceHealthchecks) {
		if kind.Key == "heartbeats" {
			return
		}
	}
	t.Fatal("heartbeats not suggested")
}

// TestHeartbeatsSlotStates: the tile says why it is empty (pending, no
// login, error) per connection, but stays quiet about a heartbeat tool
// the space has no connection for.
func TestHeartbeatsSlotStates(t *testing.T) {
	frag := &widgetlib.Fragment{View: map[string]any{}, Slots: map[string]widgetlib.Slot{
		"healthchecks": {Pending: true},
		"cronrunner":   {Error: "connection.missing"},
	}}
	rec := httptest.NewRecorder()
	err := Deps{}.Page(rec, Ctx{Locale: enums.LocaleEN}, "widgets/heartbeats", http.StatusOK, map[string]any{"ThemeURL": "", "Frag": frag})
	if err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data-pending") || strings.Contains(body, "is-error") {
		t.Fatalf("slot states:\n%s", body)
	}
}
