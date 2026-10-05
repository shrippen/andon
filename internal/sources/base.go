// Package sources turns one query against one service into JSON-able
// domain data:
//
//	Get("rss").Fetch(ctx) -> {"items": [...]}
//
// Sources never see users or permissions; services decide who may ask.
package sources

import (
	"context"
	"fmt"
	"time"

	"andon/internal/drivers/httpclient"
	"andon/internal/enums"
)

// SourceError is an expected failure (service down, bad token); its
// message is shown to the user.
type SourceError struct{ msg string }

func (e SourceError) Error() string { return e.msg }

func newSourceError(format string, args ...any) SourceError {
	return SourceError{fmt.Sprintf(format, args...)}
}

// Ctx is what a source needs to run one fetch: the connection's own
// settings plus per-call parameters (e.g. an RSS feed's URL and limit).
type Ctx struct {
	URL       string
	Secret    string
	VerifyTLS bool
	Options   map[string]any
	Params    map[string]any
	Events    []Pushed // push sources only, oldest first
}

// TLS is the connection's certificate check for driver calls.
func (c Ctx) TLS() httpclient.TLS { return httpclient.TLSOf(c.VerifyTLS) }

// TLS says whether a call checks the server's certificate.
type TLS = httpclient.TLS

// TLSFor maps a connection's "verify TLS" setting, for calls made
// outside a fetch (sign-in flows, token renewal).
func TLSFor(verify bool) TLS { return httpclient.TLSOf(verify) }

// Pushed is one event a service sent to the dashboard's webhook.
type Pushed struct {
	Event, Subject string
	At             time.Time
}

// PushSource is a source whose data arrives by webhook: the caller loads
// the events of the last PushWindow into Ctx.Events.
type PushSource interface {
	Source
	PushWindow() time.Duration
}

// Source is one named, cacheable query against a service.
type Source interface {
	Key() string
	TTL() time.Duration
	Service() enums.ServiceType // "" if not tied to one service (e.g. rss)
	Fetch(ctx context.Context, sctx Ctx) (any, error)
}

// source is a Source declared as a value:
//
//	var HassData = source{key: "homeassistant.data", ttl: time.Minute,
//		service: enums.ServiceHomeAssistant, fetch: fetchHass}
type source struct {
	key     string
	ttl     time.Duration
	service enums.ServiceType
	fetch   func(ctx context.Context, sctx Ctx) (any, error)
}

func (s source) Key() string                { return s.key }
func (s source) TTL() time.Duration         { return s.ttl }
func (s source) Service() enums.ServiceType { return s.service }

func (s source) Fetch(ctx context.Context, sctx Ctx) (any, error) {
	return s.fetch(ctx, sctx)
}

// pushSource is a source fed by the webhook events of its last window.
type pushSource struct {
	source
	window time.Duration
}

func (p pushSource) PushWindow() time.Duration { return p.window }

var registry = map[string]Source{}

// Register adds a source to the process-wide registry.
func Register(s Source) Source {
	registry[s.Key()] = s
	return s
}

// Get looks up a source by key.
func Get(key string) (Source, error) {
	s, ok := registry[key]
	if !ok {
		return nil, fmt.Errorf("sources: unknown source %q", key)
	}
	return s, nil
}

// dataAliases are services whose dataset source has another key.
var dataAliases = map[enums.ServiceType]string{enums.ServiceGlances: "glances"}

// DataKey is the key of a service's dataset source ("kimai.data",
// "glances").
func DataKey(service enums.ServiceType) string {
	if key, ok := dataAliases[service]; ok {
		return key
	}
	return string(service) + ".data"
}

// Usage is what a fetch's calls cost and what the service allows (see
// httpclient.Metered).
type Usage = httpclient.Usage

// Metered counts the calls a fetch makes under the returned context;
// usage reads the count so far.
func Metered(ctx context.Context) (metered context.Context, usage func() Usage) {
	metered, m := httpclient.Metered(ctx)
	return metered, m.Usage
}
