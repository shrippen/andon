package sources

// Lemmy (API v3, 0.19): the account's unread replies and mentions, the
// hot posts of its subscribed communities and its own posts. Lemmy has
// no API tokens: Andon logs in with user name and password and keeps the
// session token in memory until it is refused. Accounts with two-factor
// login cannot be read.
//
//	POST api/v3/user/login {username_or_email, password}         → {jwt}
//	GET  api/v3/site                                             → {version, my_user: {local_user_view: {person: {name, actor_id}}}}
//	GET  api/v3/user/replies?unread_only=true&sort=New           → {replies: [{comment{content, ap_id, published}, creator{name}, post{name}, community{name}}]}
//	GET  api/v3/user/mention?unread_only=true&sort=New           → {mentions: [same]}
//	GET  api/v3/post/list?type_=Subscribed&sort=Hot&limit=N      → {posts: [{post{name, url, ap_id, published}, counts{score, comments}, community{name}}]}
//	GET  api/v3/user?username=<name>&sort=New&limit=N            → {person_view: {counts: {post_count, comment_count}}, posts: [same]}

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"andon/internal/drivers/httpclient"
	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	lemmyNotes = 20
	lemmyPosts = 15
	lemmyOwn   = 20
	lemmyText  = 280

	// lemmyNaive is how older Lemmy writes times: without a zone, UTC.
	lemmyNaive = "2006-01-02T15:04:05.999999"
)

// Kinds of a LemmyNote.
const (
	LemmyReply   = "reply"
	LemmyMention = "mention"
)

// LemmyNote is an unread reply or mention.
type LemmyNote struct {
	Kind, From, Community, Post, Text, URL string
	At                                     time.Time
}

// LemmyPost is a post with its votes. URL is what it links (its own page
// for a text post), Link its page on the instance.
type LemmyPost struct {
	Community, Title, URL, Link string
	Score, Comments             int
	At                          time.Time
}

// LemmyUser is the account.
type LemmyUser struct {
	Name, URL       string
	Posts, Comments int
}

// LemmyDataset is the account's unread notes, subscribed and own posts.
type LemmyDataset struct {
	URL        string
	Version    string
	User       LemmyUser
	Replies    []LemmyNote
	Subscribed []LemmyPost
	Own        []LemmyPost
}

var LemmyData = source{key: "lemmy.data", ttl: opsTTL, service: enums.ServiceLemmy, fetch: fetchLemmy}

// lemmySessions are the session tokens by instance and credential hash.
var (
	lemmyMu       sync.Mutex
	lemmySessions = map[string]string{}
)

func fetchLemmy(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoLemmy(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	user, password, ok := strings.Cut(secret, ":")
	if !ok {
		return nil, newSourceError("credential.missing")
	}
	// A changed password must log in anew, not reuse the old session.
	key := sctx.URL + "\x00" + credKey(secret)
	jwt, err := lemmySession(ctx, sctx, key, user, password)
	if err != nil {
		return nil, err
	}

	data, err := readLemmy(ctx, services.BearerApi(sctx.URL, jwt, sctx.TLS()), sctx.URL)
	if err != nil {
		dropLemmySession(key) // an expired session answers 401; the next run logs in anew
		return nil, fetchError(err)
	}
	return data, nil
}

func readLemmy(ctx context.Context, api services.KeyedApi, base string) (*LemmyDataset, error) {
	site, err := api.Get(ctx, "api/v3/site", nil)
	if err != nil {
		return nil, err
	}
	person := asMap(asMap(asMap(asMap(site)["my_user"])["local_user_view"])["person"])
	data := &LemmyDataset{URL: base, Version: asStr(asMap(site)["version"]), User: LemmyUser{Name: asStr(person["name"]), URL: asStr(person["actor_id"])}}

	unread := url.Values{"unread_only": {"true"}, "sort": {"New"}, "limit": {strconv.Itoa(lemmyNotes)}}
	for _, kind := range []struct{ path, list, kind string }{{"api/v3/user/replies", "replies", LemmyReply}, {"api/v3/user/mention", "mentions", LemmyMention}} {
		raw, err := api.Get(ctx, kind.path, unread)
		if err != nil {
			return nil, err
		}
		for _, item := range asList(asMap(raw)[kind.list]) {
			m := asMap(item)
			c := asMap(m["comment"])
			data.Replies = append(data.Replies, LemmyNote{Kind: kind.kind, From: asStr(asMap(m["creator"])["name"]), Community: asStr(asMap(m["community"])["name"]),
				Post: asStr(asMap(m["post"])["name"]), Text: plainText(asStr(c["content"]), lemmyText), URL: asStr(c["ap_id"]), At: lemmyTime(c["published"])})
		}
	}

	hot, err := api.Get(ctx, "api/v3/post/list", url.Values{"type_": {"Subscribed"}, "sort": {"Hot"}, "limit": {strconv.Itoa(lemmyPosts)}})
	if err != nil {
		return nil, err
	}
	data.Subscribed = lemmyPostsOf(hot)

	if data.User.Name == "" {
		return data, nil
	}
	own, err := api.Get(ctx, "api/v3/user", url.Values{"username": {data.User.Name}, "sort": {"New"}, "limit": {strconv.Itoa(lemmyOwn)}})
	if err != nil {
		return nil, err
	}
	counts := asMap(asMap(asMap(own)["person_view"])["counts"])
	data.User.Posts, data.User.Comments = int(asFloat(counts["post_count"])), int(asFloat(counts["comment_count"]))
	data.Own = lemmyPostsOf(own)
	return data, nil
}

func lemmyPostsOf(raw any) []LemmyPost {
	var out []LemmyPost
	for _, item := range asList(asMap(raw)["posts"]) {
		m := asMap(item)
		p, counts := asMap(m["post"]), asMap(m["counts"])
		link := asStr(p["ap_id"])
		out = append(out, LemmyPost{Community: asStr(asMap(m["community"])["name"]), Title: asStr(p["name"]), URL: firstStr(asStr(p["url"]), link), Link: link,
			Score: int(asFloat(counts["score"])), Comments: int(asFloat(counts["comments"])), At: lemmyTime(p["published"])})
	}
	return out
}

// lemmyTime reads "2026-10-07T10:00:00.123456Z" and, from older
// instances, the same without a zone.
func lemmyTime(v any) time.Time {
	if t := parseTime(v); !t.IsZero() {
		return t
	}
	t, _ := time.Parse(lemmyNaive, asStr(v))
	return t
}

// lemmySession is a session token, from the cache or a new login.
func lemmySession(ctx context.Context, sctx Ctx, key, user, password string) (string, error) {
	lemmyMu.Lock()
	jwt, ok := lemmySessions[key]
	lemmyMu.Unlock()
	if ok {
		return jwt, nil
	}

	body, _ := json.Marshal(map[string]string{"username_or_email": user, "password": password})
	resp, err := httpclient.Request(ctx, "POST", strings.TrimRight(sctx.URL, "/")+"/api/v3/user/login", httpclient.Options{
		Body: body, SkipVerify: !sctx.VerifyTLS, Headers: map[string]string{"Content-Type": "application/json"}})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var answer struct {
		JWT   string `json:"jwt"`
		Error string `json:"error"`
	}
	if json.NewDecoder(resp.Body).Decode(&answer) != nil || answer.JWT == "" {
		return "", newSourceError("lemmy.login_failed")
	}

	lemmyMu.Lock()
	lemmySessions[key] = answer.JWT
	lemmyMu.Unlock()
	return answer.JWT, nil
}

func dropLemmySession(key string) {
	lemmyMu.Lock()
	delete(lemmySessions, key)
	lemmyMu.Unlock()
}

func DemoLemmy(now time.Time) *LemmyDataset {
	var p struct {
		URL, Version string
		User         LemmyUser
		Replies      []LemmyNote
		Subscribed   []LemmyPost
		Own          []LemmyPost
	}
	demoworld.MustDecode("lemmy", now, &p)
	return &LemmyDataset{URL: p.URL, Version: p.Version, User: p.User, Replies: p.Replies, Subscribed: p.Subscribed, Own: p.Own}
}

func init() {
	Register(LemmyData)
	Register(testOf{LemmyData, func(d any) map[string]any { return map[string]any{"unread": len(d.(*LemmyDataset).Replies)} }})
}

// credKey names a credential in an in-memory cache without holding it.
func credKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
