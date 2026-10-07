package sources

// Twitch: which of the followed channels are live, with title, game and
// viewers. An app (dev.twitch.tv/console) gives client ID and secret;
// Andon gets an app access token with them and keeps it until shortly
// before it expires. The option channels names the logins, separated by
// commas.
//
//	POST id.twitch.tv/oauth2/token?client_id&client_secret&grant_type=client_credentials → {access_token, expires_in}
//	GET  api.twitch.tv/helix/streams?user_login=a&user_login=b (Client-Id, Bearer)      → {data: [{user_login, title, game_name, viewer_count, started_at}]}

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"time"

	"andon/internal/drivers/httpclient"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	twitchTTL = 5 * time.Minute

	// twitchMaxLogins is how many logins helix/streams takes at once.
	twitchMaxLogins = 100

	// twitchTokenSlack renews a token this long before it expires.
	twitchTokenSlack = time.Hour
	twitchChannel    = "https://www.twitch.tv/"
)

var twitchIDBase = "https://id.twitch.tv"

// LiveChannel is a followed channel that streams right now.
type LiveChannel struct {
	User    string
	Title   string
	Game    string
	Viewers int
	Since   time.Time
	URL     string
}

// TwitchDataset is the live ones of the followed channels.
type TwitchDataset struct {
	URL      string
	Channels int // how many are followed
	Live     []LiveChannel
}

var TwitchData = source{key: "twitch.data", ttl: twitchTTL, service: enums.ServiceTwitch, fetch: fetchTwitch}

// twitchTokens are the app tokens by client ID.
var (
	twitchMu     sync.Mutex
	twitchTokens = map[string]twitchToken{}
)

type twitchToken struct {
	value string
	until time.Time
}

func fetchTwitch(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoTwitch(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	clientID, clientSecret, ok := strings.Cut(secret, ":")
	if !ok {
		return nil, newSourceError("credential.missing")
	}

	logins := upTo(optionList(sctx.Options["channels"]), twitchMaxLogins)
	if len(logins) == 0 {
		return nil, newSourceError("reading.no_channels")
	}
	data := &TwitchDataset{URL: sctx.URL, Channels: len(logins)}

	token, err := twitchAppToken(ctx, clientID, clientSecret)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	for _, l := range logins {
		params.Add("user_login", strings.ToLower(l))
	}
	body, _, err := httpclient.GetJSON(ctx, strings.TrimRight(sctx.URL, "/")+"/helix/streams", httpclient.Options{
		Params: params, Headers: map[string]string{"Client-Id": clientID, "Authorization": "Bearer " + token}})
	if err != nil {
		dropTwitchToken(clientID) // a revoked token answers 401; the next run asks anew
		return nil, err
	}
	for _, raw := range asList(asMap(body)["data"]) {
		m := asMap(raw)
		login := asStr(m["user_login"])
		data.Live = append(data.Live, LiveChannel{User: login, Title: asStr(m["title"]), Game: asStr(m["game_name"]),
			Viewers: int(asFloat(m["viewer_count"])), Since: parseTime(m["started_at"]), URL: twitchChannel + login})
	}
	return data, nil
}

// twitchAppToken is a valid app token, from the cache or a new one.
func twitchAppToken(ctx context.Context, clientID, clientSecret string) (string, error) {
	twitchMu.Lock()
	cached, ok := twitchTokens[clientID]
	twitchMu.Unlock()
	if ok && time.Now().Before(cached.until) {
		return cached.value, nil
	}

	text, err := httpclient.PostFormText(ctx, twitchIDBase+"/oauth2/token", httpclient.Options{Params: url.Values{
		"client_id": {clientID}, "client_secret": {clientSecret}, "grant_type": {"client_credentials"}}})
	if err != nil {
		return "", err
	}
	var answer struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if json.Unmarshal([]byte(text), &answer) != nil || answer.AccessToken == "" {
		return "", newSourceError("reading.twitch_token")
	}

	until := time.Now().Add(time.Duration(answer.ExpiresIn)*time.Second - twitchTokenSlack)
	twitchMu.Lock()
	twitchTokens[clientID] = twitchToken{value: answer.AccessToken, until: until}
	twitchMu.Unlock()
	return answer.AccessToken, nil
}

func dropTwitchToken(clientID string) {
	twitchMu.Lock()
	delete(twitchTokens, clientID)
	twitchMu.Unlock()
}

func DemoTwitch(now time.Time) *TwitchDataset {
	data := &TwitchDataset{}
	demoworld.MustDecode("twitch", now, data)
	for i, c := range data.Live {
		data.Live[i].URL = twitchChannel + c.User
	}
	data.Channels = max(len(data.Live), 3)
	return data
}

func init() {
	Register(TwitchData)
	Register(testOf{TwitchData, func(d any) map[string]any { return map[string]any{"live": len(d.(*TwitchDataset).Live)} }})
}
