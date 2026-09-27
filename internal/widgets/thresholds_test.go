package widgets

import "testing"

func TestThresholds(t *testing.T) {
	list := parseThresholds("queue > 10 gelb\nQueue >= 100 rot\nnonsense\ntemp < 5,5")
	if len(list) != 3 {
		t.Fatalf("parsed: %+v", list)
	}
	for _, c := range []struct {
		name string
		v    float64
		want string
	}{{"Queue", 5, ""}, {"queue", 11, levelWarn}, {"QUEUE", 150, levelFail}, {"temp", 3, levelWarn}, {"other", 999, ""}} {
		if got := levelOf(c.name, c.v, list); got != c.want {
			t.Errorf("%s %v: %q, want %q", c.name, c.v, got, c.want)
		}
	}
}
