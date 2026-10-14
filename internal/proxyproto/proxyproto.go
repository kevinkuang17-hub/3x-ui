// Package proxyproto reads the PROXY protocol v1 line Xray's REALITY sends (xver 1) when it hands
// an unauthenticated visitor to its target, so the target sees the visitor's address, not loopback.
package proxyproto

import (
	"bufio"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
)

// maxLine is the longest valid v1 header (107 bytes per the spec).
const maxLine = 107

// Listener wraps accepted connections; a connection that starts with a PROXY line reports the
// address in it, any other connection is passed through untouched.
type Listener struct {
	net.Listener
	HeaderTimeout time.Duration
}

func (l *Listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return wrap(c, l.HeaderTimeout), nil
}

type conn struct {
	net.Conn
	r      *bufio.Reader
	remote net.Addr
}

func (c *conn) Read(p []byte) (int, error) { return c.r.Read(p) }

func (c *conn) RemoteAddr() net.Addr {
	if c.remote != nil {
		return c.remote
	}
	return c.Conn.RemoteAddr()
}

func wrap(c net.Conn, timeout time.Duration) net.Conn {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	out := &conn{Conn: c, r: bufio.NewReaderSize(c, 512)}
	_ = c.SetReadDeadline(time.Now().Add(timeout))
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()
	peek, err := out.r.Peek(6)
	if err != nil || string(peek) != "PROXY " {
		return out
	}
	line, err := readLine(out.r)
	if err != nil {
		return out
	}
	if addr, err := parseV1(line); err == nil {
		out.remote = addr
	}
	return out
}

func readLine(r *bufio.Reader) (string, error) {
	var b strings.Builder
	for b.Len() <= maxLine {
		ch, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		b.WriteByte(ch)
		if ch == '\n' {
			return b.String(), nil
		}
	}
	return "", errors.New("proxy header too long")
}

// parseV1 reads "PROXY TCP4 <src> <dst> <sport> <dport>\r\n"; UNKNOWN carries no address.
func parseV1(line string) (net.Addr, error) {
	fields := strings.Fields(strings.TrimRight(line, "\r\n"))
	if len(fields) < 2 || fields[0] != "PROXY" {
		return nil, errors.New("not a proxy header")
	}
	if fields[1] == "UNKNOWN" {
		return nil, errors.New("unknown source")
	}
	if len(fields) != 6 || (fields[1] != "TCP4" && fields[1] != "TCP6") {
		return nil, errors.New("malformed proxy header")
	}
	ip := net.ParseIP(fields[2])
	port, err := strconv.Atoi(fields[4])
	if ip == nil || err != nil || port < 0 || port > 65535 {
		return nil, errors.New("malformed proxy address")
	}
	return &net.TCPAddr{IP: ip, Port: port}, nil
}
