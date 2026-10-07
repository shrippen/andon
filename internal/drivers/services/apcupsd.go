package services

// apcupsd's network information server (NIS, TCP 3551): a request and
// the answer lines are framed by a two-byte big-endian length; an empty
// frame ends the answer.
//
//	→ 00 06 "status"
//	← 00 1d "UPSNAME  : boje-usv\n" … 00 00
//	   "STATUS   : ONLINE", "BCHARGE  : 92.0 Percent", "TIMELEFT : 7.0 Minutes", "LOADPCT  : 61.0 Percent"

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"time"
)

const (
	apcStatus  = "status"
	apcTimeout = 10 * time.Second
	apcMaxLine = 1024
)

// ApcupsdStatus asks the NIS at addr ("host:3551") for its status lines.
func ApcupsdStatus(ctx context.Context, addr string) (map[string]string, error) {
	dialer := net.Dialer{Timeout: apcTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, ApiError{err.Error()}
	}
	defer conn.Close()
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(apcTimeout)
	}
	_ = conn.SetDeadline(deadline)

	request := binary.BigEndian.AppendUint16(nil, uint16(len(apcStatus)))
	if _, err := conn.Write(append(request, apcStatus...)); err != nil {
		return nil, ApiError{err.Error()}
	}

	out := map[string]string{}
	r := bufio.NewReader(conn)
	for {
		var size uint16
		if err := binary.Read(r, binary.BigEndian, &size); err != nil {
			return nil, ApiError{err.Error()}
		}
		if size == 0 {
			return out, nil
		}
		if size > apcMaxLine {
			return nil, ApiError{"apcupsd: frame too long"}
		}
		line := make([]byte, size)
		if _, err := io.ReadFull(r, line); err != nil {
			return nil, ApiError{err.Error()}
		}
		if key, value, ok := strings.Cut(string(line), ":"); ok {
			out[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
}
