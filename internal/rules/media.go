package rules

// Media requests:
//
//	seerr.stuck          approved requests still not available after days
//	cross.requests_arr   stuck requests while Sonarr or Radarr report problems
//	                     (health checks or stuck downloads): likely the cause

import (
	"andon/internal/enums"
	"andon/internal/sources"
)

var seerrSvc = string(enums.ServiceSeerr)

func init() {
	Register("seerr.stuck", seerrSvc, nil, on(seerrStuck))
	Register("cross.requests_arr", Cross, nil, requestsArr)
}

func stuckTitles(data *sources.SeerrDataset) []string {
	var out []string
	for _, r := range data.Stuck {
		out = append(out, r.Title)
	}
	return out
}

func seerrStuck(data *sources.SeerrDataset, _ map[string]any, _ Env) []Finding {
	if len(data.Stuck) == 0 {
		return nil
	}
	return []Finding{svcFinding(seerrSvc, "seerr.stuck", "stuck", "seerr.stuck", enums.SeverityWarn, data.URL,
		map[string]any{"count": len(data.Stuck), "titles": shortList(stuckTitles(data))})}
}

func requestsArr(_ any, _ map[string]any, env Env) []Finding {
	seerr, ok := env.Datasets[seerrSvc].(*sources.SeerrDataset)
	arr, ok2 := env.Datasets[string(enums.ServiceArr)].(*sources.ArrDataset)
	if !ok || !ok2 || len(seerr.Stuck) == 0 || (len(arr.Health) == 0 && len(arr.Stuck) == 0) {
		return nil
	}
	problem := ""
	if len(arr.Health) > 0 {
		problem = arr.Health[0].Message
	} else {
		problem = arr.Stuck[0]
	}
	return []Finding{{Fingerprint: "requests_arr", Severity: enums.SeverityWarn, Message: "cross.requests_arr",
		Params:  map[string]any{"count": len(seerr.Stuck), "app": orDash(arr.App), "problem": problem},
		Sources: []string{seerrSvc, string(enums.ServiceArr)}}}
}
