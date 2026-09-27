// Package httpclient is the shared HTTP client with timeouts and an egress
// guard. Every outbound request of the app passes the guard, so the admin
// can restrict reachable networks and invited users cannot scan the
// internal network through status checks or feeds.
package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	ConnectTimeout = 5 * time.Second
	ReadTimeout    = 15 * time.Second
	UserAgent      = "andon/0.1 (+https://github.com/shrippen/dashboard)"
	MaxBody        = 5 * 1024 * 1024
)

// HttpError is a transport or status failure with a short, secret-free
// message (never wraps a raw error that might contain a token or URL).
type HttpError struct{ msg string }

func (e HttpError) Error() string { return e.msg }

// EgressDenied means the guard rejected the target host/addresses.
type EgressDenied struct{ Host string }

func (e EgressDenied) Error() string { return "egress denied: " + e.Host }

// Guard decides whether a host (already resolved to addrs) may be reached.
// A nil guard (the default) allows everything.
type Guard func(host string, addrs []net.IP) bool

var guard atomic.Pointer[Guard]

// SetGuard installs the process-wide egress guard, or nil to allow all.
// Safe while requests run: the admin changes the policy at any time.
func SetGuard(g Guard) {
	if g == nil {
		guard.Store(nil)
		return
	}
	guard.Store(&g)
}

func currentGuard() Guard {
	if g := guard.Load(); g != nil {
		return *g
	}
	return nil
}

// checkGuard rejects a URL early, before any connection. The binding
// check happens again at dial time (dialGuarded), on the address
// actually connected to.
func checkGuard(rawURL string) error {
	g := currentGuard()
	if g == nil {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return HttpError{"bad url"}
	}
	addrs, err := net.LookupIP(u.Hostname())
	if err != nil {
		return HttpError{"dns: " + u.Hostname()}
	}
	if !g(u.Hostname(), addrs) {
		return EgressDenied{u.Hostname()}
	}
	return nil
}

// Dial connects to addr ("host:port") through the egress guard. The host
// is resolved once and the checked address is dialed, so neither a
// redirect nor a DNS answer that changes in between (rebinding) reaches
// a denied network:
//
//	resolve host → guard(host, ips) → dial ip:port
func Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: ConnectTimeout, KeepAlive: keepAlive}
	g := currentGuard()
	if g == nil {
		return dialer.DialContext(ctx, network, addr)
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, HttpError{"bad host"}
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, HttpError{"dns: " + host}
	}
	if !g(host, ips) {
		return nil, EgressDenied{host}
	}

	// Every address passed the guard; try them in order ("localhost" →
	// ::1, then 127.0.0.1).
	var last error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

// Shared transports, one per TLS mode: connections are kept alive and
// reused instead of a handshake per request.
const (
	keepAlive       = 30 * time.Second
	idleConnTimeout = 90 * time.Second
	maxIdlePerHost  = 4
)

var transports = map[TLS]*http.Transport{
	TLSVerify: newTransport(TLSVerify),
	TLSSkip:   newTransport(TLSSkip),
}

func newTransport(mode TLS) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = Dial
	t.TLSClientConfig = &tls.Config{InsecureSkipVerify: mode == TLSSkip} //nolint:gosec // opt-in per connection
	t.TLSHandshakeTimeout = ConnectTimeout
	t.IdleConnTimeout = idleConnTimeout
	t.MaxIdleConnsPerHost = maxIdlePerHost
	return t
}

// Options configure one Request call.
type Options struct {
	Headers map[string]string
	Params  url.Values
	// Body is the raw request body (e.g. a POST's JSON payload). nil for
	// none.
	Body []byte
	// SkipVerify disables TLS certificate verification. Defaults to false
	// (verified) so a zero-value Options is always safe; set true only for
	// a connection the user explicitly marked as self-signed.
	SkipVerify bool
	Timeout    time.Duration
	// NoRedirect returns a redirect as is, e.g. to read a login cookie.
	NoRedirect bool
}

// Request performs one guarded HTTP call and returns the raw response. The
// caller must close resp.Body.
func Request(ctx context.Context, method, rawURL string, opts Options) (*http.Response, error) {
	if err := checkGuard(rawURL); err != nil {
		return nil, err
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, HttpError{"bad url"}
	}
	if opts.Params != nil {
		u.RawQuery = opts.Params.Encode()
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = ReadTimeout
	}
	client := &http.Client{Timeout: timeout, Transport: transports[TLSOf(!opts.SkipVerify)]}
	if opts.NoRedirect {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}

	var body io.Reader
	if opts.Body != nil {
		body = bytes.NewReader(opts.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, HttpError{"bad request"}
	}
	req.Header.Set("User-Agent", UserAgent)
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	sent := time.Now()
	resp, err := client.Do(req)
	var denied EgressDenied
	if errors.As(err, &denied) {
		return nil, denied
	}
	if err != nil {
		return nil, transportError(err, u.Hostname())
	}
	noteClock(u.Hostname(), resp.Header.Get("Date"), sent, time.Now())
	return resp, nil
}

// ── Clock skew ──
//
// Every HTTP answer carries the server's time (Date header, 1 s steps).
// Compared with the middle of the request it shows a host whose clock is
// off, which breaks TOTP, certificates and schedules:
//
//	sent 10:00:00.0 · Date 09:55:00 · received 10:00:00.4  →  skew ≈ -5 min

var (
	clockMu sync.Mutex
	clocks  = map[string]time.Duration{}
)

func noteClock(host, date string, sent, received time.Time) {
	at, err := http.ParseTime(date)
	if err != nil {
		return
	}
	mid := sent.Add(received.Sub(sent) / 2)
	clockMu.Lock()
	clocks[strings.ToLower(host)] = at.Sub(mid).Truncate(time.Second)
	clockMu.Unlock()
}

// ClockSkew is how far host's clock was off at the last answer: negative
// when it runs behind. ok=false if the host never sent a Date header.
func ClockSkew(host string) (time.Duration, bool) {
	clockMu.Lock()
	defer clockMu.Unlock()
	skew, ok := clocks[strings.ToLower(host)]
	return skew, ok
}

// transportError names why a request failed without the raw error, which
// may carry the full URL: "dns: api.example.org", "connection refused:
// 10.0.0.5", "tls certificate: …".
func transportError(err error, host string) HttpError {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return HttpError{"timeout: " + host}
	case errors.As(err, &dnsErr):
		return HttpError{"dns: " + host}
	case errors.Is(err, syscall.ECONNREFUSED):
		return HttpError{"connection refused: " + host}
	case errors.As(err, &unknownCA), errors.As(err, &hostnameErr), errors.As(err, &certErr):
		return HttpError{"tls certificate: " + host}
	default:
		return HttpError{"request failed: " + host}
	}
}

// GetJSON performs a guarded GET and decodes a JSON body, returning the
// response headers too (callers need X-Total-Pages etc.).
func GetJSON(ctx context.Context, rawURL string, opts Options) (any, http.Header, error) {
	resp, err := Request(ctx, http.MethodGet, rawURL, opts)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return nil, nil, HttpError{"read failed"}
	}
	if len(body) > MaxBody {
		return nil, nil, HttpError{"response too large"}
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, resp.Header, HttpError{fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}

	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, resp.Header, HttpError{"invalid JSON"}
	}
	return parsed, resp.Header, nil
}

// GetText performs a guarded GET and returns the body as text (e.g.
// Prometheus metrics).
func GetText(ctx context.Context, rawURL string, opts Options) (string, error) {
	return doText(ctx, http.MethodGet, rawURL, opts)
}

// PostFormText performs a guarded POST with opts.Params as an
// x-www-form-urlencoded body (e.g. a ClientLogin endpoint that 404s a GET)
// and returns the body as text.
func PostFormText(ctx context.Context, rawURL string, opts Options) (string, error) {
	form := opts.Params.Encode()
	opts.Body = []byte(form)
	opts.Params = nil
	if opts.Headers == nil {
		opts.Headers = map[string]string{}
	}
	opts.Headers["Content-Type"] = "application/x-www-form-urlencoded"
	return doText(ctx, http.MethodPost, rawURL, opts)
}

func doText(ctx context.Context, method, rawURL string, opts Options) (string, error) {
	resp, err := Request(ctx, method, rawURL, opts)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return "", HttpError{"read failed"}
	}
	if len(body) > MaxBody {
		return "", HttpError{"response too large"}
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return "", HttpError{fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	return string(body), nil
}

// PeerCert connects to host:port over TLS and returns the leaf
// certificate without verifying it, so expired ones can be reported too.
func PeerCert(ctx context.Context, hostPort string) (*x509.Certificate, error) {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		return nil, HttpError{"bad host"}
	}
	if err := checkGuard("https://" + hostPort); err != nil {
		return nil, err
	}

	raw, err := Dial(ctx, "tcp", hostPort)
	if err != nil {
		return nil, HttpError{"connect failed"}
	}
	conn := tls.Client(raw, &tls.Config{ServerName: host, InsecureSkipVerify: true}) //nolint:gosec // only reads the certificate
	defer conn.Close()
	if err := conn.HandshakeContext(ctx); err != nil {
		return nil, HttpError{"connect failed"}
	}

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, HttpError{"no certificate"}
	}
	return certs[0], nil
}

// Client returns an http.Client whose requests pass the egress guard.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: transports[TLSVerify]}
}

// TLS says whether a request checks the server's certificate.
type TLS int

const (
	TLSVerify TLS = iota // check it (the default)
	TLSSkip              // accept self-signed homelab certificates
)

// TLSOf maps a connection's "verify TLS" setting.
func TLSOf(verify bool) TLS {
	if verify {
		return TLSVerify
	}
	return TLSSkip
}

// ClientTLS is Client with TLS verification switchable per connection
// (self-signed homelab certificates).
func ClientTLS(timeout time.Duration, mode TLS) *http.Client {
	return &http.Client{Timeout: timeout, Transport: transports[mode]}
}

// CheckHost applies the egress guard to a non-HTTP connection (IMAP).
func CheckHost(host string) error {
	return checkGuard("https://" + host)
}
