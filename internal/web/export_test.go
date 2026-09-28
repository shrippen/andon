package web

import "andon/internal/services/hints"

// Test-only access to the hints page helpers.
var GroupHints = func(v []hints.View) []hintGroup { return groupHints(v, groupRule) }

type HintFilter = hintFilter

func (f hintFilter) Apply(all []hints.View) []hints.View { return f.apply(all) }

// FormSecret exposes how the connection form builds a secret.
var FormSecret = formSecret
