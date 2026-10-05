package sources

// KDE Store (store.kde.org, the OCS API of Pling): published Plasma
// widgets, themes and the like with their download counts. No token.
//
//	options user: shrippen        every entry of that user
//	        ids: [2368175, …]     single entries (listed first)
//
//	GET ocs/v1/content/data?user=…&page=0…  GET ocs/v1/content/data/<id>

import (
	"context"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	storeTTL      = time.Hour // counts change slowly; the API tells no quota
	storePageSize = 100
	storePages    = 10
	storeData     = "ocs/v1/content/data"
	storeOK       = "ok" // OCS status of an answer with data
)

// StoreItem is one published entry.
type StoreItem struct {
	ID           int64
	Name         string
	Version      string
	Type         string // "Plasma 6 Applets"
	URL          string // its store page
	Downloads    int    // 0 = unknown
	Changed      time.Time
	Repos        []string      // GitHub repos its description links: "shrippen/plasmai"
	DownloadDays []DownloadDay // demo only, see DownloadItem.Days
}

// KDEStoreDataset is a user's and the named entries.
type KDEStoreDataset struct {
	URL   string
	User  string
	Items []StoreItem
}

var KDEStoreData = source{key: "kdestore.data", ttl: storeTTL, service: enums.ServiceKDEStore, fetch: fetchKDEStore}

func fetchKDEStore(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoKDEStore(time.Now().UTC()), nil
	}
	api := services.BearerApi(sctx.URL, "", sctx.TLS())
	data := &KDEStoreDataset{URL: sctx.URL, User: strings.TrimSpace(asStr(sctx.Options["user"]))}
	seen := map[int64]bool{}
	add := func(list []any) {
		for _, raw := range list {
			item := storeItem(asMap(raw))
			if item.ID != 0 && !seen[item.ID] {
				seen[item.ID] = true
				data.Items = append(data.Items, item)
			}
		}
	}

	for _, raw := range asList(sctx.Options["ids"]) {
		id := strconv.FormatInt(int64(asFloat(raw)), 10)
		list, _, err := storeGet(ctx, api, storeData+"/"+id, nil)
		if err != nil {
			return nil, fetchError(err)
		}
		add(list)
	}
	if data.User == "" {
		return data, nil
	}

	// The server may hand out fewer per page than asked: totalitems ends.
	read := 0
	for page := range storePages {
		params := url.Values{"user": {data.User}, "pagesize": {strconv.Itoa(storePageSize)}, "page": {strconv.Itoa(page)}}
		list, total, err := storeGet(ctx, api, storeData, params)
		if err != nil {
			return nil, fetchError(err)
		}
		add(list)
		read += len(list)
		if len(list) == 0 || read >= total {
			break
		}
	}
	return data, nil
}

// storeGet asks the OCS API for JSON and returns its data list and how
// many there are in all; a status other than "ok" (unknown entry) is an
// error.
func storeGet(ctx context.Context, api services.KeyedApi, path string, params url.Values) ([]any, int, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("format", "json")
	raw, err := api.Get(ctx, path, params)
	if err != nil {
		return nil, 0, err
	}
	answer := asMap(raw)
	if asStr(answer["status"]) != storeOK {
		return nil, 0, newSourceError("kdestore.not_found")
	}
	return asList(answer["data"]), int(asFloat(answer["totalitems"])), nil
}

func storeItem(m map[string]any) StoreItem {
	return StoreItem{ID: int64(asFloat(m["id"])), Name: asStr(m["name"]), Version: asStr(m["version"]), Type: asStr(m["typename"]),
		URL: asStr(m["detailpage"]), Downloads: int(asFloat(m["downloads"])), Changed: parseTime(m["changed"]),
		Repos: githubLinks(asStr(m["description"]))}
}

// githubLink is a repo link in an entry's description:
// "https://github.com/shrippen/Plasmai/issues" names shrippen/plasmai.
var githubLink = regexp.MustCompile(`github\.com/([\w.-]+)/([\w.-]+)`)

// githubLinks are the repos a description links, lower case, each once.
func githubLinks(description string) []string {
	var out []string
	for _, m := range githubLink.FindAllStringSubmatch(description, -1) {
		repo := strings.ToLower(m[1] + "/" + strings.TrimSuffix(m[2], ".git"))
		if !slices.Contains(out, repo) {
			out = append(out, repo)
		}
	}
	return out
}

// Downloads lists every entry.
func (d *KDEStoreDataset) Downloads() []DownloadItem {
	var out []DownloadItem
	for _, it := range d.Items {
		out = append(out, DownloadItem{ID: strconv.FormatInt(it.ID, 10), Name: it.Name, URL: it.URL, Version: it.Version,
			Released: it.Changed, Total: it.Downloads, Days: it.DownloadDays})
	}
	return out
}

func DemoKDEStore(now time.Time) *KDEStoreDataset {
	var p struct {
		URL, User string
		Items     []struct {
			StoreItem
			Typename       string
			Detailpage     string
			DownloadsDaily []int
		}
	}
	demoworld.MustDecode("code.kde_store", now, &p)
	data := &KDEStoreDataset{URL: p.URL, User: p.User}
	for _, it := range p.Items {
		item := it.StoreItem
		item.Type, item.URL = it.Typename, it.Detailpage
		item.DownloadDays = daysBack(item.Downloads, it.DownloadsDaily, now)
		data.Items = append(data.Items, item)
	}
	return data
}

func init() {
	Register(KDEStoreData)
	Register(testOf{KDEStoreData, func(d any) map[string]any { return map[string]any{"items": len(d.(*KDEStoreDataset).Items)} }})
}
