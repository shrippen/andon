package sources

// CI across providers: a dataset that knows builds names each repository's
// state in one shape, so the CI tile and the release check read Drone,
// GitHub Actions and Gitea Actions the same way.
//
//	DroneDataset (builds), GitHubDataset (latest run), GiteaDataset (latest failed run) ─► CIRepos()

import (
	"strings"
	"time"

	"andon/internal/enums"
)

// CIStatus is a run's outcome.
type CIStatus string

const (
	CIOK      CIStatus = "ok"
	CIFailed  CIStatus = "failed"
	CIRunning CIStatus = "running"
	CIOther   CIStatus = "other" // skipped, killed, blocked …
)

// CIRun is one build or workflow run.
type CIRun struct {
	Number  int
	Status  CIStatus
	Event   string // push, tag, pull_request, cron …
	Started time.Time
	Seconds int
	Commit  string // the built commit, "" unknown
}

// CIRepo is a repository's CI on its default branch.
type CIRepo struct {
	Provider enums.ServiceType
	Repo     string // "studio/website"
	URL      string
	Status   CIStatus // of the newest run
	Step     string   // a failed run's failing step, "" unknown
	Runs     []CIRun  // newest first; empty when the provider tells only the newest state
}

// CISource is a dataset that reports CI.
type CISource interface{ CIRepos() []CIRepo }

// ciStatus maps the providers' words: Drone's success/failure/error,
// GitHub's conclusions success/failure/cancelled/timed_out.
func ciStatus(word string) CIStatus {
	switch strings.ToLower(word) {
	case "success":
		return CIOK
	case "failure", "error", "timed_out":
		return CIFailed
	case "running", "pending", "in_progress", "queued":
		return CIRunning
	}
	return CIOther
}

// CIRepos are the repos with a latest run on their default branch.
func (d *GitHubDataset) CIRepos() []CIRepo {
	var out []CIRepo
	for _, r := range d.Repos {
		if r.CI == "" {
			continue
		}
		repo := CIRepo{Provider: enums.ServiceGitHub, Repo: r.Name, URL: r.CIURL, Status: ciStatus(r.CI), Step: r.CIStep}
		// The latest run is history too, when GitHub said when it ran.
		if !r.CIAt.IsZero() {
			repo.Runs = []CIRun{{Status: repo.Status, Event: "push", Started: r.CIAt, Commit: r.CICommit}}
		}
		out = append(out, repo)
	}
	return out
}

// CIRepos are the repos whose latest Actions run failed; Gitea tells
// nothing about the others.
func (d *GiteaDataset) CIRepos() []CIRepo {
	var out []CIRepo
	for _, r := range d.Repos {
		if r.FailedWorkflow != "" {
			out = append(out, CIRepo{Provider: enums.ServiceGitea, Repo: strings.TrimPrefix(r.Name, "/"), URL: r.URL, Status: CIFailed, Step: r.FailedWorkflow})
		}
	}
	return out
}
