package sources

// Picture widgets: the latest xkcd and NASA's astronomy picture of the
// day. Images are inlined like the image widget's, so the browser never
// contacts the image host.

import (
	"cmp"
	"context"
	"math/rand/v2"
	"net/url"
	"strconv"
	"time"

	"andon/internal/drivers/httpclient"
)

const (
	pictureTTL = 6 * time.Hour
	apodMax    = 4 << 20 // APOD images are larger than icons and photos
	nasaDemo   = "DEMO_KEY"
	apodImage  = "image"
)

var (
	xkcdBase    = "https://xkcd.com"
	explainBase = "https://www.explainxkcd.com/wiki/index.php/"
	nasaBase    = "https://api.nasa.gov"
)

// Picture is a titled image; DataURI is "" when only Link can be shown
// (a video, or an image too large to inline).
type Picture struct {
	Title, Text, Link, DataURI string
	Explain                    string // xkcd: the comic on explainxkcd.com
	Day                        string // APOD: its date, "2026-09-30"
}

// ── xkcd ──

// xkcdMissing is the comic number that does not exist (a joke of its own).
const xkcdMissing = 404

var XkcdSource = source{key: "xkcd", ttl: pictureTTL, fetch: fetchXkcd}

func fetchXkcd(ctx context.Context, sctx Ctx) (any, error) {
	body, _, err := httpclient.GetJSON(ctx, xkcdBase+"/info.0.json", httpclient.Options{})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	m := asMap(body)
	if latest := int(asFloat(m["num"])); asBool(sctx.Params["random"]) && latest > 1 {
		n := rand.IntN(latest) + 1 //nolint:gosec // picks a comic, nothing secret
		if n == xkcdMissing {
			n++
		}
		if n <= latest {
			one, _, err := httpclient.GetJSON(ctx, xkcdBase+"/"+strconv.Itoa(n)+"/info.0.json", httpclient.Options{})
			if err != nil {
				return nil, newSourceError("%s", err.Error())
			}
			m = asMap(one)
		}
	}
	num := strconv.Itoa(int(asFloat(m["num"])))
	pic := &Picture{Title: asStr(m["safe_title"]), Text: asStr(m["alt"]), Link: xkcdBase + "/" + num + "/", Explain: explainBase + num}
	img, err := fetchImage(ctx, asStr(m["img"]), imageMax)
	if err != nil {
		return nil, err
	}
	pic.DataURI = img.DataURI
	return pic, nil
}

// ── apod ──

var ApodSource = source{key: "apod", ttl: pictureTTL, fetch: fetchApod}

// Fetch uses the widget's own API key or NASA's rate-limited DEMO_KEY.
func fetchApod(ctx context.Context, sctx Ctx) (any, error) {
	key := asStr(sctx.Params["api_key"])
	if key == "" {
		key = nasaDemo
	}
	body, _, err := httpclient.GetJSON(ctx, nasaBase+"/planetary/apod", httpclient.Options{Params: url.Values{"api_key": {key}}})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	m := asMap(body)
	pic := &Picture{Title: asStr(m["title"]), Text: asStr(m["explanation"]), Link: asStr(m["url"])}
	if asStr(m["media_type"]) != apodImage {
		return pic, nil
	}

	// A picture that fails to inline still shows as a link.
	if img, err := fetchImage(ctx, pic.Link, apodMax); err == nil {
		pic.DataURI = img.DataURI
	}
	return pic, nil
}

// PictureList is a few pictures, newest first.
type PictureList struct{ Items []Picture }

// apodArchiveDays is how many earlier pictures the APOD dialog shows.
const apodArchiveDays = 7

// ApodArchiveSource reads the pictures of the days before today, when the
// dialog opens; videos and pictures too large to inline are left out.
var ApodArchiveSource = source{key: "apod.archive", ttl: pictureTTL, fetch: fetchApodArchive}

func fetchApodArchive(ctx context.Context, sctx Ctx) (any, error) {
	key := cmp.Or(asStr(sctx.Params["api_key"]), nasaDemo)
	today := time.Now().UTC()
	params := url.Values{"api_key": {key}, "start_date": {today.AddDate(0, 0, -apodArchiveDays).Format(time.DateOnly)},
		"end_date": {today.AddDate(0, 0, -1).Format(time.DateOnly)}}
	body, _, err := httpclient.GetJSON(ctx, nasaBase+"/planetary/apod", httpclient.Options{Params: params})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	var pics []Picture
	for _, raw := range asList(body) {
		m := asMap(raw)
		if asStr(m["media_type"]) == apodImage {
			pics = append(pics, Picture{Title: asStr(m["title"]), Link: asStr(m["url"]), Day: asStr(m["date"])})
		}
	}

	// Inline each picture, at most imageMax: the archive shows thumbnails.
	parallel(ctx, len(pics), releasesParallel, func(i int) {
		if img, err := fetchImage(ctx, pics[i].Link, imageMax); err == nil {
			pics[i].DataURI = img.DataURI
		}
	})
	out := &PictureList{}
	for i := len(pics) - 1; i >= 0; i-- {
		if pics[i].DataURI != "" {
			out.Items = append(out.Items, pics[i])
		}
	}
	return out, nil
}

func init() {
	Register(XkcdSource)
	Register(ApodSource)
	Register(ApodArchiveSource)
}
