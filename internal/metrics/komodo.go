package metrics

import "strings"

// KomodoStopped reads a Komodo connection's "stopped" option: stacks off
// on purpose, e.g. {stopped: [cloudbeaver, filezilla]}. Names are lower
// case.
func KomodoStopped(options map[string]any) map[string]bool {
	out := map[string]bool{}
	list, _ := options["stopped"].([]any)
	for _, v := range list {
		if name, ok := v.(string); ok && name != "" {
			out[strings.ToLower(name)] = true
		}
	}
	return out
}
