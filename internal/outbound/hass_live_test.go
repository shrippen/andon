package outbound_test

import (
	"context"
	"testing"

	"andon/internal/drivers/services"
	"andon/internal/outbound"
	"andon/internal/testkit/live"
)

// The throwaway Home Assistant's own switch (configuration.yaml); real
// entities switch real devices, so this runs against it only.
const (
	hassSwitch = "input_boolean.andon_test"
	kindEntity = "entity"
)

// A tile's switch action: toggled twice, back where it was.
func TestHassToggleLive(t *testing.T) {
	to := live.Target(t, live.Hass)
	api := services.HassApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	ctx := context.Background()
	live.Created(t, live.Hass, kindEntity, hassSwitch)

	before := hassState(t, api)
	for _, want := range []string{flip(before), before} {
		live.Change(t, live.Hass, live.Update, kindEntity, hassSwitch)
		if err := outbound.HassToggle(ctx, to, hassSwitch); err != nil {
			t.Fatalf("toggle: %v", err)
		}
		if got := hassState(t, api); got != want {
			t.Errorf("state = %s, want %s", got, want)
		}
	}
}

// hassState reads the switch's state, "on" or "off".
func hassState(t *testing.T, api services.HassApi) string {
	t.Helper()
	raw, err := api.States(context.Background())
	if err != nil {
		t.Fatalf("states: %v", err)
	}
	list, _ := raw.([]any)
	for _, e := range list {
		if m := asObject(e); m["entity_id"] == hassSwitch {
			s, _ := m["state"].(string)
			return s
		}
	}
	t.Skipf("no %s in Home Assistant", hassSwitch)
	return ""
}

func flip(state string) string {
	if state == "on" {
		return "off"
	}
	return "on"
}
