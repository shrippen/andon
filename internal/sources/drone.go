package sources

// Drone CI: each active repository's builds, newest first. A personal
// token (Drone → user settings) is enough.
//
//	GET api/user/repos?latest=true                → [{slug, default_branch, active, build}]
//	GET api/repos/<owner>/<name>/builds?per_page=N → [{number, status, event, target, after, started, finished}]

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	droneRepos    = "api/user/repos"
	droneBuilds   = 10 // builds read per repo
	droneMaxRepos = 40
	droneParallel = 4
	dronePullReq  = "pull_request"
)

// DroneRepo is one repository with its default branch's builds.
type DroneRepo struct {
	Repo, Branch string
	Builds       []CIRun // newest first
}

// DroneDataset is the user's active repositories.
type DroneDataset struct {
	URL   string
	Repos []DroneRepo
}

var DroneData = source{key: "drone.data", ttl: opsTTL, service: enums.ServiceDrone, fetch: fetchDrone}

func fetchDrone(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoDrone(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.BearerApi(sctx.URL, secret, sctx.TLS())
	raw, err := api.Get(ctx, droneRepos, url.Values{"latest": {"true"}})
	if err != nil {
		return nil, fetchError(err)
	}
	data := &DroneDataset{URL: sctx.URL}
	for _, item := range asList(raw) {
		r := asMap(item)
		if active, _ := r["active"].(bool); !active || len(data.Repos) >= droneMaxRepos {
			continue
		}
		data.Repos = append(data.Repos, DroneRepo{Repo: asStr(r["slug"]), Branch: asStr(r["default_branch"])})
	}

	errs := make([]error, len(data.Repos))
	parallel(ctx, len(data.Repos), droneParallel, func(i int) {
		repo := &data.Repos[i]
		list, err := api.Get(ctx, "api/repos/"+repo.Repo+"/builds", url.Values{"per_page": {strconv.Itoa(droneBuilds)}})
		if err != nil {
			errs[i] = err
			return
		}
		for _, b := range asList(list) {
			m := asMap(b)
			if t := asStr(m["target"]); t != "" && repo.Branch != "" && t != repo.Branch && asStr(m["event"]) != "tag" {
				continue // another branch
			}
			started, finished := asFloat(m["started"]), asFloat(m["finished"])
			run := CIRun{Number: int(asFloat(m["number"])), Status: ciStatus(asStr(m["status"])), Event: asStr(m["event"]), Commit: asStr(m["after"])}
			if started > 0 {
				run.Started = time.Unix(int64(started), 0).UTC()
			}
			if finished > started {
				run.Seconds = int(finished - started)
			}
			repo.Builds = append(repo.Builds, run)
		}
	})
	if err := errors.Join(errs...); err != nil {
		return nil, fetchError(err)
	}
	return data, nil
}

// pushed are a repo's builds of its branch itself, not of pull requests.
func (r DroneRepo) pushed() []CIRun {
	var out []CIRun
	for _, b := range r.Builds {
		if b.Event != dronePullReq {
			out = append(out, b)
		}
	}
	return out
}

// CIRepos are the repos with their branch's builds.
func (d *DroneDataset) CIRepos() []CIRepo {
	var out []CIRepo
	for _, r := range d.Repos {
		runs := r.pushed()
		if len(runs) == 0 {
			continue
		}
		out = append(out, CIRepo{Provider: enums.ServiceDrone, Repo: r.Repo, URL: d.URL + "/" + r.Repo, Status: runs[0].Status, Runs: runs})
	}
	return out
}

// DemoDrone is Studio Weber's builds.
func DemoDrone(now time.Time) *DroneDataset {
	var p struct {
		URL   string
		Repos []struct {
			Repo, Branch string
			Builds       []struct {
				Number, Seconds       int
				Status, Event, Commit string
				Started               time.Time
			}
		}
	}
	demoworld.MustDecode("ci", now, &p)
	data := &DroneDataset{URL: p.URL}
	for _, r := range p.Repos {
		repo := DroneRepo{Repo: r.Repo, Branch: r.Branch}
		for _, b := range r.Builds {
			repo.Builds = append(repo.Builds, CIRun{Number: b.Number, Status: ciStatus(b.Status), Event: b.Event, Started: b.Started, Seconds: b.Seconds, Commit: b.Commit})
		}
		data.Repos = append(data.Repos, repo)
	}
	return data
}

func init() {
	Register(DroneData)
	Register(testOf{DroneData, func(d any) map[string]any { return map[string]any{"repos": len(d.(*DroneDataset).Repos)} }})
}
