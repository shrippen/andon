// Package workbench is what every workbench shares. A workbench works
// through a service's open cases with the user and writes only what the
// user confirmed (first: vatcheck):
//
//	cases ─► ticked steps ─► Review: each checked, spelled out
//	      ─► confirm ─► Apply: checked again against fresh data,
//	                    written in order, one audit entry each
//	      ─► Result: done, skipped, failed with their reasons
//
// A bench loads its data and knows how to check and write one step; the
// order, the refusals and the bookkeeping live here once.
package workbench

import (
	"context"
	"database/sql"
	"errors"
	"maps"

	"andon/internal/services/access"
	auditsvc "andon/internal/services/audit"
)

// ActionSkip sets a case aside without booking it; it counts as skipped.
const ActionSkip = "skip"

// ErrNothing means no step was ticked.
var ErrNothing = errors.New("workbench.err_nothing")

// Step is one ticked row: its case, what to do and the row's fields,
// e.g. {Case: "b3", Action: "create", Fields: {"vendor": "Nordhost"}}.
type Step struct {
	Case   string
	Action string
	Fields map[string]string
}

// Field is one of the row's fields, "" if missing.
func (s Step) Field(name string) string { return s.Fields[name] }

// Problem says why a step cannot be written: an i18n key, "" none.
type Problem string

const ProblemNone Problem = ""

// Planned is a step as it would be written; Item is what the bench
// resolved for the review (the transaction, the expense …).
type Planned[P any] struct {
	Step
	Item    P
	Problem Problem
}

// Plan is the review: what Apply would write.
type Plan[P any] struct {
	Steps []Planned[P]
}

// Writes counts the steps that would be written.
func (p Plan[P]) Writes() int {
	n := 0
	for _, s := range p.Steps {
		if s.Problem == ProblemNone {
			n++
		}
	}
	return n
}

// Checker resolves a step against a bench's loaded data; a Problem
// keeps it unwritten. It may change the step's fields, e.g. normalise a
// tax rate.
type Checker[P any] interface {
	Check(s *Step) (P, Problem)
}

// Bench is one workbench's loaded data and what only it knows.
type Bench[P any] interface {
	Checker[P]
	// Write writes one sound step and names what it wrote against.
	Write(ctx context.Context, p Planned[P]) (string, error)
	// Name names a case where it failed, e.g. the payee.
	Name(p Planned[P]) string
	// Reason is a refused write's text: an i18n key or the service's message.
	Reason(err error) string
	// Audit is what the audit entry of a written step records.
	Audit(p Planned[P]) map[string]any
}

// PlanOf checks every step; the steps' fields are copied first.
func PlanOf[P any](b Checker[P], steps []Step) (Plan[P], error) {
	if len(steps) == 0 {
		return Plan[P]{}, ErrNothing
	}
	var out Plan[P]
	for _, s := range steps {
		s.Fields = maps.Clone(s.Fields)
		if s.Fields == nil {
			s.Fields = map[string]string{}
		}
		item, problem := b.Check(&s)
		out.Steps = append(out.Steps, Planned[P]{Step: s, Item: item, Problem: problem})
	}
	return out, nil
}

// Result is what Apply wrote.
type Result struct {
	Done, Skipped int
	Failed        []Failure
}

// Failure is a step not written, or written with a follow-up refused.
type Failure struct {
	Case, Name string
	Reason     string // i18n key or the service's message
}

// Partly is a step written whose follow-up was refused, e.g. a new
// expense whose scan could not be linked: done, and listed as failed.
type Partly struct {
	Target string
	Reason string // i18n key
}

func (p Partly) Error() string { return p.Reason }

// Apply writes the plan's sound steps in order; a refused one is listed
// and the rest go on. A written step leaves the audit entry
// "<audit>.<action>", e.g. "vatcheck.link".
func Apply[P any](ctx context.Context, d *sql.DB, who *access.Principal, b Bench[P], plan Plan[P], audit, ip string) Result {
	var out Result
	for _, p := range plan.Steps {
		fail := func(reason string) {
			out.Failed = append(out.Failed, Failure{Case: p.Case, Name: b.Name(p), Reason: reason})
		}
		if p.Problem != ProblemNone {
			fail(string(p.Problem))
			continue
		}

		target, err := b.Write(ctx, p)
		var partly Partly
		switch {
		case errors.As(err, &partly):
			target = partly.Target
			fail(partly.Reason)
		case err != nil:
			fail(b.Reason(err))
			continue
		}

		if p.Action == ActionSkip {
			out.Skipped++
		} else {
			out.Done++
		}
		_ = auditsvc.Log(d, &who.UserID, audit+"."+p.Action, target, ip, b.Audit(p))
	}
	return out
}
