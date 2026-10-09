package connections

// Why a connection test failed, read from the driver's short message
// (httpclient: "connection refused: kimai.lan", "egress denied: 10.0.0.5",
// "HTTP 401", …), so the page can explain it in the user's language and
// keep the raw text as detail.

import (
	"regexp"
	"strings"
)

// Cause is a known reason a test failed; CauseNone if unknown.
type Cause string

const (
	CauseNone      Cause = ""
	CauseRefused   Cause = "refused"   // nothing listens on host:port
	CauseEgress    Cause = "egress"    // Andon's network policy blocks the host
	CauseAuth      Cause = "auth"      // HTTP 401: token or login wrong
	CauseForbidden Cause = "forbidden" // HTTP 403: login lacks rights
	CauseTLS       Cause = "tls"       // certificate not trusted
	CauseDNS       Cause = "dns"       // host name not found
	CauseTimeout   Cause = "timeout"   // no answer in time
	CauseMoved     Cause = "moved"     // redirected to another server
)

// movedMark is httpclient.Moved's message; its group the target.
var movedMark = regexp.MustCompile(`\bredirected to (\S+): use it as the URL`)

// egressShort is the whole message a web check gives for a blocked host.
const egressShort = "egress"

// causeMarks finds a cause in a message, first match wins.
var causeMarks = []struct {
	cause Cause
	mark  *regexp.Regexp
}{
	{CauseEgress, regexp.MustCompile(`\begress denied\b`)},
	{CauseMoved, movedMark},
	{CauseRefused, regexp.MustCompile(`\bconnection refused\b`)},
	{CauseAuth, regexp.MustCompile(`\bHTTP 401\b`)},
	{CauseForbidden, regexp.MustCompile(`\bHTTP 403\b`)},
	{CauseTLS, regexp.MustCompile(`\btls certificate\b|\bx509\b`)},
	{CauseDNS, regexp.MustCompile(`\bdns:`)},
	{CauseTimeout, regexp.MustCompile(`\btimeout\b`)},
}

// CauseOf reads the cause of a failed test from its message.
func CauseOf(msg string) Cause {
	if strings.TrimSpace(msg) == egressShort {
		return CauseEgress
	}
	for _, c := range causeMarks {
		if c.mark.MatchString(msg) {
			return c.cause
		}
	}
	return CauseNone
}

// MovedTo is the server a message says the connection was redirected
// to, e.g. "https://ghostfolio.example"; "" for any other message.
func MovedTo(msg string) string {
	m := movedMark.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	return m[1]
}
