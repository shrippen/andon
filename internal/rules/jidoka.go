package rules

// Jidoka's own state, besides its jobs (heartbeat.down covers those):
//
//	jidoka.invalid        job files Jidoka could not load: they do not run
//	jidoka.channel_down   a sending channel fails: Jidoka cannot tell anyone

import (
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

var jidokaSvc = string(enums.ServiceJidoka)

func init() {
	Register("jidoka.invalid", jidokaSvc, nil, on(jidokaInvalid))
	Register("jidoka.channel_down", jidokaSvc, nil, on(jidokaChannelDown))
}

func jidokaInvalid(d *sources.JidokaDataset, _ map[string]any, _ Env) []Finding {
	var found []Finding
	for _, in := range d.Instances {
		if in.Invalid == 0 {
			continue
		}
		f := svcFinding(jidokaSvc, "jidoka.invalid", "invalid", "jidoka.invalid", enums.SeverityWarn, in.URL,
			map[string]any{"count": in.Invalid, "names": strings.Join(in.InvalidJobs, ", ")})
		f.ConnID = in.ConnID
		found = append(found, f)
	}
	return found
}

func jidokaChannelDown(d *sources.JidokaDataset, _ map[string]any, _ Env) []Finding {
	var found []Finding
	for _, in := range d.Instances {
		for _, c := range in.Channels {
			if !c.Down {
				continue
			}
			f := svcFinding(jidokaSvc, "jidoka.channel_down", "channel:"+c.Name, "jidoka.channel_down", enums.SeverityWarn, in.URL,
				map[string]any{"channel": c.Name, "since": agoParam(c.Since)})
			f.ConnID = in.ConnID
			found = append(found, f)
		}
	}
	return found
}
