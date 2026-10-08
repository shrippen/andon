package sources

// Fediverse (Mastodon API: Mastodon, GoToSocial, Akkoma): the account's
// followers, its notifications (unread ones marked), its own posts and
// the instance's version. Token: an application with read access
// (Mastodon: Preferences → Development).
//
//	GET api/v1/instance                                 → {version}
//	GET api/v1/accounts/verify_credentials              → {id, acct, display_name, url, followers_count, following_count, statuses_count}
//	GET api/v1/notifications?limit=30                   → [{id, type, created_at, account{acct}, status{content, url}}]
//	GET api/v1/markers?timeline[]=notifications         → {notifications: {last_read_id}}
//	GET api/v1/accounts/<id>/statuses?limit=40&exclude_reblogs=true → [{content, url, created_at, card{url}}]

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	fediNotes   = 30
	fediPosts   = 40
	fediTextMax = 280
)

// fediSoftware: words in an instance's version or source URL that name
// its software, e.g. "2.7.2 (compatible; Akkoma 3.15.1)".
var fediSoftware = map[string]string{"akkoma": "Akkoma", "pleroma": "Pleroma", "gotosocial": "GoToSocial"}

// FediAccount is the token's account.
type FediAccount struct {
	Acct, Name, URL             string
	Followers, Following, Posts int
	FollowerDays                []DownloadDay // demo only: earlier day totals, oldest first
}

// fediTypes are the notification types shown by name; the rest is
// fediOther (admin reports, polls of others, …).
var fediTypes = map[string]bool{"mention": true, "status": true, "reblog": true, "follow": true, "follow_request": true, "favourite": true, "poll": true, "update": true}

const fediOther = "other"

// FediNote is one notification of a fediTypes type or fediOther.
type FediNote struct {
	Type, From, Text, URL string
	At                    time.Time
	Unread                bool
}

// FediPost is one of the account's own posts; Link is its preview
// card's address.
type FediPost struct {
	Text, URL, Link string
	At              time.Time
}

// FediverseDataset is the account and its instance.
type FediverseDataset struct {
	URL      string
	Software string // "" = Mastodon or unknown
	Version  string
	Account  FediAccount
	Notes    []FediNote
	Posts    []FediPost
	// Marker: the instance tells what was read; without it no
	// notification counts as unread.
	Marker bool
}

// Unread lists the unread notifications of a type, "" = all.
func (d *FediverseDataset) Unread(kind string) []FediNote {
	var out []FediNote
	for _, n := range d.Notes {
		if n.Unread && (kind == "" || n.Type == kind) {
			out = append(out, n)
		}
	}
	return out
}

var FediverseData = source{key: "fediverse.data", ttl: opsTTL, service: enums.ServiceFediverse, fetch: fetchFediverse}

func fetchFediverse(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoFediverse(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.BearerApi(sctx.URL, secret, sctx.TLS())
	me, err := api.Get(ctx, "api/v1/accounts/verify_credentials", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	m := asMap(me)
	data := &FediverseDataset{URL: sctx.URL, Account: FediAccount{Acct: asStr(m["acct"]), Name: firstStr(asStr(m["display_name"]), asStr(m["acct"])), URL: asStr(m["url"]),
		Followers: int(asFloat(m["followers_count"])), Following: int(asFloat(m["following_count"])), Posts: int(asFloat(m["statuses_count"]))}}

	if inst, err := api.Get(ctx, "api/v1/instance", nil); err == nil {
		data.Version = asStr(asMap(inst)["version"])
		data.Software = softwareOf(data.Version)
	}
	// GoToSocial names itself only in the v2 instance's source_url.
	if data.Software == "" {
		if inst, err := api.Get(ctx, "api/v2/instance", nil); err == nil {
			data.Software = softwareOf(asStr(asMap(inst)["source_url"]))
		}
	}

	lastRead := ""
	if markers, err := api.Get(ctx, "api/v1/markers", url.Values{"timeline[]": {"notifications"}}); err == nil {
		lastRead = asStr(asMap(asMap(markers)["notifications"])["last_read_id"])
		data.Marker = lastRead != ""
	}
	notes, err := api.Get(ctx, "api/v1/notifications", url.Values{"limit": {strconv.Itoa(fediNotes)}})
	if err != nil {
		return nil, fetchError(err)
	}
	for _, item := range asList(notes) {
		n := asMap(item)
		status := asMap(n["status"])
		kind := asStr(n["type"])
		if !fediTypes[kind] {
			kind = fediOther
		}
		data.Notes = append(data.Notes, FediNote{Type: kind, From: asStr(asMap(n["account"])["acct"]), Text: plainText(asStr(status["content"]), fediTextMax),
			URL: asStr(status["url"]), At: parseTime(n["created_at"]), Unread: data.Marker && laterID(asStr(n["id"]), lastRead)})
	}

	posts, err := api.Get(ctx, "api/v1/accounts/"+url.PathEscape(asStr(m["id"]))+"/statuses", url.Values{"limit": {strconv.Itoa(fediPosts)}, "exclude_reblogs": {"true"}})
	if err != nil {
		return nil, fetchError(err)
	}
	for _, item := range asList(posts) {
		p := asMap(item)
		data.Posts = append(data.Posts, FediPost{Text: plainText(asStr(p["content"]), fediTextMax), URL: asStr(p["url"]), Link: asStr(asMap(p["card"])["url"]), At: parseTime(p["created_at"])})
	}
	return data, nil
}

// softwareOf names the software from the version, "" when it is none of
// fediSoftware.
func softwareOf(version string) string {
	lower := strings.ToLower(version)
	for word, name := range fediSoftware {
		if strings.Contains(lower, word) {
			return name
		}
	}
	return ""
}

// laterID: id comes after last. Mastodon's ids are growing numbers as
// text; others sort as text.
func laterID(id, last string) bool {
	if len(id) != len(last) && strings.Trim(id+last, "0123456789") == "" {
		return len(id) > len(last)
	}
	return id > last
}

func DemoFediverse(now time.Time) *FediverseDataset {
	var p struct {
		URL, Software, Version string
		Account                struct {
			Acct, DisplayName, URL         string
			Followers, Following, Statuses int
			FollowersDaily                 []int
		}
		Notifications []FediNote
		Statuses      []FediPost
	}
	demoworld.MustDecode("fediverse", now, &p)
	a := p.Account
	data := &FediverseDataset{URL: p.URL, Software: p.Software, Version: p.Version, Marker: true, Notes: p.Notifications, Posts: p.Statuses,
		Account: FediAccount{Acct: a.Acct, Name: a.DisplayName, URL: a.URL, Followers: a.Followers, Following: a.Following, Posts: a.Statuses,
			FollowerDays: daysBack(a.Followers, a.FollowersDaily, now)}}
	for i, s := range data.Posts {
		data.Posts[i].Link = firstLink(s.Text)
	}
	return data
}

// firstLink is the first web address in a text, "" without one.
func firstLink(text string) string {
	for _, word := range strings.Fields(text) {
		if strings.HasPrefix(word, "https://") || strings.HasPrefix(word, "http://") {
			return strings.TrimRight(word, ".,;:!?)")
		}
	}
	return ""
}

func init() {
	Register(FediverseData)
	Register(testOf{FediverseData, func(d any) map[string]any {
		return map[string]any{"followers": d.(*FediverseDataset).Account.Followers}
	}})
}
