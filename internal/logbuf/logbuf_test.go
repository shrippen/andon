package logbuf

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestKeepsAndPassesOn(t *testing.T) {
	var out bytes.Buffer
	log := slog.New(Wrap(slog.NewTextHandler(&out, nil))).With("job", "analysis")

	log.Info("started")
	log.Warn("job failed", "err", "timeout")

	if !strings.Contains(out.String(), "job failed") {
		t.Fatalf("not passed on: %q", out.String())
	}
	warns := Since(slog.LevelWarn)
	if len(warns) == 0 || warns[0].Msg != "job failed" || warns[0].Attrs != "job=analysis err=timeout" {
		t.Fatalf("warnings = %+v", warns)
	}
	all := Since(slog.LevelInfo)
	if len(all) < 2 || all[1].Msg != "started" {
		t.Fatalf("all = %+v", all)
	}
}

func TestRingIsCapped(t *testing.T) {
	log := slog.New(Wrap(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	for i := range Max + 10 {
		log.Info(fmt.Sprint("m", i))
	}
	all := Since(slog.LevelDebug)
	if len(all) != Max || all[0].Msg != fmt.Sprint("m", Max+9) {
		t.Fatalf("kept %d, newest %q", len(all), all[0].Msg)
	}
}
