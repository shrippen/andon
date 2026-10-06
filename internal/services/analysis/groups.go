package analysis

// Verbünde in the analysis: rules that read other services' datasets
// see those of their own Verbund, not "the last Kimai of the space".

import (
	"maps"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	linkrepo "andon/internal/repos/links"
	"andon/internal/rules"
	"andon/internal/services/verbund"
	"andon/internal/sources"
)

// vagueRule names services with several connections and no Verbund
// that says which belongs with which.
const vagueRule = "system.partner_ambiguous"

// split builds the scope's Verbund groups from its fetched connections;
// call it once the space-wide datasets are in.
//
// links reads the customer links of a Kimai and an Invoice Ninja that
// share a group (verbund.ClientMapFor), for the cross rules.
func (sc *scope) split(stored []linkrepo.Link, links func(kimai, ninja int64) metrics.ClientMap) {
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
		kimai, ninja := g.Conns[string(enums.ServiceKimai)], g.Conns[string(enums.ServiceInvoiceNinja)]
		if kimai != nil && ninja != nil && links != nil {
			out.datasets[rules.ClientMapDataset] = links(kimai.ID, ninja.ID)
		}
		sc.groups = append(sc.groups, fanOut(out, g, byID)...)
	}
}

// fanOut adds the services read space-wide (Group.Fan): datasets that
// merge (sources.Merger: Docker hosts, Borg servers …) join as one, the
// space's; for the others the group repeats, round i taking each one's
// i-th connection, so every one is read once (findings merge by
// fingerprint).
func fanOut(base group, g verbund.Group, byID map[int64]run) []group {
	rounds := map[string][]*model.Connection{}
	n := 0
	for service, list := range g.Fan {
		if _, own := g.Conns[service]; own {
			continue
		}
		if merged, ok := mergeAll(list, byID); ok {
			for _, c := range list {
				base.conns[c.ID] = true
			}
			base.datasets[service] = merged
			base.options[service] = list[0].Options
			continue
		}
		rounds[service] = list
		n = max(n, len(list))
	}
	if n == 0 {
		return []group{base}
	}
	out := make([]group, 0, n)
	for i := range n {
		round := group{conns: maps.Clone(base.conns), datasets: maps.Clone(base.datasets), options: maps.Clone(base.options)}
		for service, list := range rounds {
			c := list[i%len(list)]
			round.conns[c.ID] = true
			round.datasets[service] = byID[c.ID].result.Data
			round.options[service] = c.Options
		}
		out = append(out, round)
	}
	return out
}

// mergeAll merges the fetched datasets of list, false if they do not
// merge. Connections without data are left out.
func mergeAll(list []*model.Connection, byID map[int64]run) (any, bool) {
	var out any
	for _, c := range list {
		data := byID[c.ID].result.Data
		if data == nil {
			continue
		}
		if out == nil {
			if _, ok := data.(sources.Merger); !ok {
				return nil, false
			}
			out = data
			continue
		}
		out = out.(sources.Merger).Merge(data)
	}
	return out, out != nil
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
