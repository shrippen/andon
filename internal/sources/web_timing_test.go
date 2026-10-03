package sources

import (
	"context"
	"net/http/httptrace"
	"testing"
	"time"
)

// TestServiceTimeSkipsDNS: a status check's ms is the service's answer
// time; waiting for the resolver (5 s on a dropped packet) is left out.
func TestServiceTimeSkipsDNS(t *testing.T) {
	const dnsWait = 60 * time.Millisecond
	ctx, took := serviceTimer(context.Background())

	trace := httptrace.ContextClientTrace(ctx)
	trace.DNSStart(httptrace.DNSStartInfo{Host: "svc.test"})
	time.Sleep(dnsWait)
	trace.DNSDone(httptrace.DNSDoneInfo{})

	if got := took(); got >= dnsWait/2 {
		t.Fatalf("expected the DNS wait left out, got %v", got)
	}
}
