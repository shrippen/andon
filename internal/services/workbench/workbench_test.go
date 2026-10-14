package workbench_test

import (
	"context"
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/workbench"
	"andon/internal/testkit"
)

// bench is a fake: case "gone" has a problem, "refused" is refused,
// "half" is written with its follow-up refused.
type bench struct{ written []string }

func (b *bench) Check(s *workbench.Step) (string, workbench.Problem) {
	s.Fields["seen"] = "1"
	if s.Case == "gone" {
		return "", "x.problem_gone"
	}
	return "item " + s.Case, workbench.ProblemNone
}

func (b *bench) Write(_ context.Context, p workbench.Planned[string]) (string, error) {
	switch p.Case {
	case "refused":
		return "", errors.New("no")
	case "half":
		b.written = append(b.written, p.Case)
		return "", workbench.Partly{Target: "E-1", Reason: "x.problem_scan"}
	}
	b.written = append(b.written, p.Case)
	return p.Item, nil
}

func (b *bench) Name(p workbench.Planned[string]) string        { return "name " + p.Case }
func (b *bench) Reason(err error) string                        { return "x." + err.Error() }
func (b *bench) Audit(workbench.Planned[string]) map[string]any { return nil }

func TestPlanOf(t *testing.T) {
	if _, err := workbench.PlanOf[string](&bench{}, nil); !errors.Is(err, workbench.ErrNothing) {
		t.Fatalf("no steps: %v", err)
	}

	// Check may change the fields; the caller's map stays as it was.
	fields := map[string]string{"vendor": "Nordhost"}
	plan, err := workbench.PlanOf[string](&bench{}, []workbench.Step{{Case: "a", Action: "link", Fields: fields}, {Case: "gone", Action: "link"}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Writes() != 1 || plan.Steps[0].Item != "item a" || plan.Steps[1].Problem != "x.problem_gone" {
		t.Fatalf("plan: %+v", plan)
	}
	if fields["seen"] != "" || plan.Steps[0].Field("seen") != "1" {
		t.Errorf("fields: caller %v, plan %v", fields, plan.Steps[0].Fields)
	}
}

// A problem or a refusal stops only its step; a refused follow-up still
// counts as done; set aside counts as skipped.
func TestApply(t *testing.T) {
	d := testkit.DB(t)
	who, _ := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	b := &bench{}
	steps := []workbench.Step{{Case: "a", Action: "link"}, {Case: "gone", Action: "link"}, {Case: "refused", Action: "link"},
		{Case: "half", Action: "create"}, {Case: "p", Action: workbench.ActionSkip}}
	plan, err := workbench.PlanOf[string](b, steps)
	if err != nil {
		t.Fatal(err)
	}

	res := workbench.Apply[string](context.Background(), d, who, b, plan, "x", "")
	if res.Done != 2 || res.Skipped != 1 || len(res.Failed) != 3 {
		t.Fatalf("result: %+v", res)
	}
	want := []workbench.Failure{{Case: "gone", Name: "name gone", Reason: "x.problem_gone"}, {Case: "refused", Name: "name refused", Reason: "x.no"},
		{Case: "half", Name: "name half", Reason: "x.problem_scan"}}
	for i, f := range want {
		if res.Failed[i] != f {
			t.Errorf("failure %d: %+v, want %+v", i, res.Failed[i], f)
		}
	}
	if len(b.written) != 3 {
		t.Errorf("written: %v", b.written)
	}
}

type note struct {
	Conn  int64             `json:"conn"`
	Marks map[string]string `json:"marks"`
}

func TestState(t *testing.T) {
	d := testkit.DB(t)
	who, _ := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	if s, err := workbench.LoadState[note](d, who, "bench_x"); err != nil || s.Conn != 0 {
		t.Fatalf("empty: %+v %v", s, err)
	}
	err := workbench.SaveState(d, who, "bench_x", func(s *note) { s.Conn, s.Marks = 3, map[string]string{"a": "b"} })
	if err != nil {
		t.Fatal(err)
	}
	if s, err := workbench.LoadState[note](d, who, "bench_x"); err != nil || s.Conn != 3 || s.Marks["a"] != "b" {
		t.Fatalf("stored: %+v %v", s, err)
	}
}
