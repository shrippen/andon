package metrics

// Compose repos against Komodo (Phase 15): a stack in a repo that Komodo
// does not run, and a stack Komodo runs that no repo holds.
//
//	compose  regis/immich  regis/kometa            Komodo  immich   (repo …/docker-compose-regis)
//	                                                        sure     (server Regis)
//	                       └ not deployed ┘                 └ deployed, unknown ┘
//
// A Komodo stack's host is the repo it is linked to (docker-compose-regis
// → regis), else its server's name; names compare folded ("Plötze" =
// "ploetze"). Stacks match by host and name: Komodo's stack name must
// be the stack's directory. Only hosts both sides know are compared: a
// host without compose repo, or one Komodo runs nothing on, says nothing.

import (
	"strings"

	"andon/internal/sources"
)

// Komodo stack states that say nothing about a deployment: down (no
// containers) counts as not deployed, unknown (server unreachable) is
// skipped on both sides.
const (
	komodoDown    = "down"
	komodoUnknown = "unknown"
)

// Deployed is a stack Komodo runs, on the host of the compose repos.
type Deployed struct {
	Host  string // compose host, "regis"
	Stack sources.KStack
}

// DeployCheck is the comparison of compose stacks and Komodo.
type DeployCheck struct {
	NotDeployed []sources.Stack // in a repo, Komodo runs it not
	Unknown     []Deployed      // Komodo runs it, no repo holds it
	Compared    int             // repo stacks on hosts Komodo runs stacks on
}

// CheckDeploys compares; ok is false while stacks are unknown or no
// Komodo is read.
func CheckDeploys(data *sources.GiteaDataset, komodo *sources.KomodoDataset) (DeployCheck, bool) {
	if data == nil || !data.StacksRead || komodo == nil {
		return DeployCheck{}, false
	}

	// Compose hosts by folded name, and Komodo's stacks per host.
	hosts := map[string]string{}
	for _, s := range data.Stacks {
		hosts[hostKey(s.Host)] = s.Host
	}
	running := map[string]map[string]sources.KStack{}
	for _, k := range komodo.Stacks {
		host := hostKey(komodoHost(k))
		if host == "" {
			continue
		}
		if running[host] == nil {
			running[host] = map[string]sources.KStack{}
		}
		running[host][strings.ToLower(k.Name)] = k
	}

	var out DeployCheck
	inRepo := map[string]bool{}
	for _, s := range data.Stacks {
		host := hostKey(s.Host)
		inRepo[host+"/"+strings.ToLower(s.Name)] = true
		on, known := running[host]
		if !known {
			continue
		}
		out.Compared++
		k, ok := on[strings.ToLower(s.Name)]
		if !ok || k.State == komodoDown {
			out.NotDeployed = append(out.NotDeployed, s)
		}
	}

	for _, k := range komodo.Stacks {
		host := hostKey(komodoHost(k))
		name, known := hosts[host]
		if !known || k.State == komodoDown || k.State == komodoUnknown || inRepo[host+"/"+strings.ToLower(k.Name)] {
			continue
		}
		out.Unknown = append(out.Unknown, Deployed{Host: name, Stack: k})
	}
	return out, true
}

// komodoHost is the host a Komodo stack runs on: its compose repo's host,
// else its server; "" if neither is known.
func komodoHost(k sources.KStack) string {
	if name := repoName(k.Repo); strings.HasPrefix(name, sources.ComposePrefix) {
		return strings.TrimPrefix(name, sources.ComposePrefix)
	}
	return k.Server
}

// hostFold spells umlauts as compose repo names do: Plötze → ploetze.
var hostFold = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")

// hostKey is a host name for comparison: lower case, umlauts folded.
func hostKey(host string) string {
	return hostFold.Replace(strings.ToLower(strings.TrimSpace(host)))
}

// Finding ids of the comparison, shared with Hansei like the others.
const (
	notDeployedPrefix = "docs.not_deployed:"
	unknownPrefix     = "docs.deployed_unknown:"
)

// NotDeployedID names a stack in a repo that Komodo does not run.
func NotDeployedID(s sources.Stack) string { return notDeployedPrefix + s.Host + "/" + s.Name }

// UnknownID names a stack Komodo runs that no repo holds.
func UnknownID(d Deployed) string { return unknownPrefix + d.Host + "/" + d.Stack.Name }
