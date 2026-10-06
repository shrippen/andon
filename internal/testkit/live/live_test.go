package live

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// local points the package at an empty .local-test/ in a temp dir.
func local(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv(dirEnv, d)
	return d
}

func TestTargetSkipsWithoutConfig(t *testing.T) {
	local(t)
	ran := false
	t.Run("kimai", func(t *testing.T) {
		Target(t, Kimai)
		ran = true
	})
	if ran {
		t.Fatal("test ran without a configured instance")
	}
}

func TestTargetReadsConfig(t *testing.T) {
	d := local(t)
	conf := `{"url": "https://kimai.example/", "token": "tok", "verifyTLS": false}`
	if err := os.WriteFile(filepath.Join(d, "kimai.json"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}

	to := Target(t, Kimai)
	if to.URL != "https://kimai.example" || to.Token != "tok" || to.VerifyTLS {
		t.Fatalf("target = %+v", to)
	}
}

func TestChangeOnlyOwnEntries(t *testing.T) {
	d := local(t)
	Created(t, Kimai, "place", 7)
	Change(t, Kimai, Update, "place", 7)

	// Same id of another kind or service: not ours.
	for _, c := range []struct {
		svc  Service
		kind string
	}{{Kimai, "timesheet"}, {Dawarich, "place"}} {
		if own(t, c.svc, c.kind, 7) {
			t.Errorf("%s %s 7 counted as own", c.svc, c.kind)
		}
	}

	log, err := os.ReadFile(filepath.Join(d, logName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "kimai\tupdate\tplace\t7\tTestChangeOnlyOwnEntries") {
		t.Fatalf("log = %q", log)
	}
}

func TestNameMarksTestEntries(t *testing.T) {
	if n := Name(t); !strings.HasPrefix(n, namePrefix+" TestNameMarksTestEntries ") {
		t.Fatalf("name = %q", n)
	}
}
