package analysis

// Verbünde in the analysis: rules that read other services' datasets
// see those of their own Verbund, not "the last Kimai of the space".

import (
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	linkrepo "andon/internal/repos/links"
	"andon/internal/rules"
	"andon/internal/services/verbund"
)

// vagueRule names services with several connections and no Verbund
// that says which belongs with which.
const vagueRule = "system.partner_ambiguous"

// split builds the scope's Verbund groups from its fetched connections;
// call it once the space-wide datasets are in.
func (sc *scope) split(stored []linkrepo.Link) {
	byID := map[int64]run{}
	var conns []*model.Connection
	for _, r := range sc.fetched {
		if _, ok := byID[r.conn.ID]; ok {
			continue
		}
		byID[r.conn.ID] = r
		conns = append(conns, r.conn)
	}

	groups, vague := verbund.Groups(conns, stored)
	sc.vague = vague
	sc.groups = nil
	for _, g := range groups {
		out := group{conns: map[int64]bool{}, datasets: sc.base(conns), options: map[string]map[string]any{}}
		for service, c := range g.Conns {
			r := byID[c.ID]
			out.conns[c.ID] = true
			out.datasets[service] = r.result.Data
			out.options[service] = c.Options
		}
		sc.groups = append(sc.groups, out)
	}
}

// base is the scope's datasets without any service's: what every
// Verbund shares (failures, links, clocks, history).
func (sc *scope) base(conns []*model.Connection) map[string]any {
	services := map[string]bool{}
	for _, c := range conns {
		services[c.Service] = true
	}
	out := map[string]any{}
	for k, v := range sc.datasets {
		if !services[k] {
			out[k] = v
		}
	}
	return out
}

// envsOf are the envs a connection's own rules run in: one per Verbund
// it is in; outside every Verbund (an ambiguous service) its own dataset
// with the shared ones.
func (sc *scope) envsOf(r run, settings map[string]any, today time.Time) []rules.Env {
	var out []rules.Env
	for _, g := range sc.groups {
		if g.conns[r.conn.ID] {
			out = append(out, rules.Env{Today: today, Settings: settings, Datasets: g.datasets, Options: g.options})
		}
	}
	if len(out) > 0 {
		return out
	}

	var conns []*model.Connection
	for _, f := range sc.fetched {
		conns = append(conns, f.conn)
	}
	datasets := sc.base(conns)
	datasets[r.conn.Service] = r.result.Data
	return []rules.Env{{Today: today, Settings: settings, Datasets: datasets,
		Options: map[string]map[string]any{r.conn.Service: r.conn.Options}}}
}

// vagueFindings asks to put connections into Verbünde: one hint per
// space naming the services.
func vagueFindings(services []string) []rules.Finding {
	if len(services) == 0 {
		return nil
	}
	return []rules.Finding{{
		Fingerprint: "vague:" + strings.Join(services, ","), Rule: vagueRule, Severity: enums.SeverityInfo,
		Message: vagueRule, Params: map[string]any{"services": strings.Join(services, ", ")}, Sources: services,
	}}
}
