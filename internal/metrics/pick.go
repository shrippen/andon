package metrics

// Pick is one value a learned model suggests for a form field, shown
// under it ("Vorschlag: Hosting, Software"): Value is what the field
// takes, Label what the user reads, or Text an i18n key when the value
// has a fixed name ("vatcheck.no_tax").
type Pick struct{ Value, Label, Text string }
