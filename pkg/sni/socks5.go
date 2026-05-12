package sni

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// dialSOCKS5 opens a SOCKS5 tunnel on proxy, then requests CONNECT to
// host:port. Returns the live tunnel net.Conn.
//
// We use SOCKS5 NO_AUTH (0x00) since the upstream is always localhost and
// the authentication is enforced by the Shadowsocks outbound further down.
func dialSOCKS5(proxy, host string, port uint16) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", proxy, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial socks5 %s: %w", proxy, err)
	}
	// Handshake deadline only; cleared before returning to caller.
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Greeting: VER(1)=5  NMETHODS(1)=1  METHOD(1)=NO_AUTH
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 greeting: %w", err)
	}
	br := bufio.NewReader(conn)
	reply := make([]byte, 2)
	if _, err := io.ReadFull(br, reply); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 greeting reply: %w", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks5 greeting rejected: %v", reply)
	}

	// Request: VER(5) CMD(1=CONNECT) RSV(0) ATYP(3=DOMAIN) LEN(1) DOMAIN PORT(2)
	if len(host) > 255 {
		conn.Close()
		return nil, errors.New("hostname too long for socks5")
	}
	req := make([]byte, 0, 7+len(host))
	req = append(req, 0x05, 0x01, 0x00, 0x03, byte(len(host)))
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, port)

	if _, err := conn.Write(req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect write: %w", err)
	}

	// Response: VER REP RSV ATYP BND_ADDR BND_PORT
	head := make([]byte, 4)
	if _, err := io.ReadFull(br, head); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 reply: %w", err)
	}
	if head[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect rejected: code %d", head[1])
	}
	// Skip BND_ADDR depending on ATYP, plus BND_PORT(2).
	switch head[3] {
	case 0x01: // IPv4
		if _, err := io.CopyN(io.Discard, br, 4+2); err != nil {
			conn.Close()
			return nil, err
		}
	case 0x03: // DOMAIN
		lenByte := make([]byte, 1)
		if _, err := io.ReadFull(br, lenByte); err != nil {
			conn.Close()
			return nil, err
		}
		if _, err := io.CopyN(io.Discard, br, int64(lenByte[0])+2); err != nil {
			conn.Close()
			return nil, err
		}
	case 0x04: // IPv6
		if _, err := io.CopyN(io.Discard, br, 16+2); err != nil {
			conn.Close()
			return nil, err
		}
	default:
		conn.Close()
		return nil, fmt.Errorf("unexpected ATYP %d", head[3])
	}

	// If bufio buffered more than the reply, stitch those bytes back onto the conn.
	if br.Buffered() > 0 {
		pre, _ := br.Peek(br.Buffered())
		conn = &prefixConn{Conn: conn, pre: append([]byte(nil), pre...)}
	}

	conn.SetDeadline(time.Time{})
	return conn, nil
}

// prefixConn lets us prepend bytes onto a net.Conn for Read without losing
// data consumed by the SOCKS5 bufio reader.
type prefixConn struct {
	net.Conn
	pre []byte
}

func (c *prefixConn) Read(b []byte) (int, error) {
	if len(c.pre) > 0 {
		n := copy(b, c.pre)
		c.pre = c.pre[n:]
		return n, nil
	}
	return c.Conn.Read(b)
}
