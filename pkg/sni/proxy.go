// Package sni is a transparent SNI-preserving TLS proxy.
//
// Motivation: sing-box's `direct` inbound does not rewrite the connection
// destination using the sniffed SNI. When Kiro connects to app.kiro.dev:443
// (which /etc/hosts points to 127.0.0.1), sing-box sees the destination as
// 127.0.0.1:443 and tries to forward that to EC2 — useless.
//
// This proxy reads enough of the TLS ClientHello to extract the SNI, opens a
// SOCKS5 tunnel to the local sing-box with the real hostname as the
// destination, replays the already-read bytes, and splices the rest.
package sni

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

const (
	// tlsRecordMaxBytes is the largest size we'll buffer before giving up.
	// Real ClientHello messages are under 4 KB; 16 KB is plenty of headroom.
	tlsRecordMaxBytes = 16 * 1024
	sniffDeadline     = 10 * time.Second
)

// Server listens on an address and proxies each connection by SNI.
type Server struct {
	Addr        string // "127.0.0.1:443"
	SocksAddr   string // upstream sing-box SOCKS5 "127.0.0.1:1080"
	ReadTimeout time.Duration
	Logger      *log.Logger
}

// Run blocks serving until ln accept returns an error.
func (s *Server) Run() error {
	if s.ReadTimeout == 0 {
		s.ReadTimeout = sniffDeadline
	}
	if s.Logger == nil {
		s.Logger = log.Default()
	}

	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.Addr, err)
	}
	s.Logger.Printf("sni proxy listening on %s, upstream socks5 %s", s.Addr, s.SocksAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return fmt.Errorf("accept: %w", err)
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(client net.Conn) {
	defer client.Close()
	client.SetReadDeadline(time.Now().Add(s.ReadTimeout))

	// Peek enough bytes to extract SNI, but remember everything — it has to
	// be forwarded to the upstream verbatim.
	buffered := bufio.NewReader(client)
	sni, err := extractSNI(buffered)
	if err != nil {
		s.Logger.Printf("sni sniff failed from %s: %v", client.RemoteAddr(), err)
		return
	}

	// Reset the deadline: we've got what we need.
	client.SetReadDeadline(time.Time{})

	remote, err := dialSOCKS5(s.SocksAddr, sni, 443)
	if err != nil {
		s.Logger.Printf("socks5 dial %s:443 failed: %v", sni, err)
		return
	}
	defer remote.Close()

	s.Logger.Printf("proxy %s -> %s:443 (via %s)", client.RemoteAddr(), sni, s.SocksAddr)

	// Now splice. The ClientHello bytes are already in the buffered reader;
	// we have to drain them before plain io.Copy, so wrap the remaining
	// stream back together.
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(remote, buffered)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(client, remote)
		errc <- err
	}()
	<-errc
}

// extractSNI parses the TLS ClientHello out of r without advancing past it
// (it reads enough bytes that buffering holds them for the subsequent copy).
//
// Record layout:
//
//	ContentType(1)=0x16 Handshake
//	Version(2)
//	Length(2)                          total handshake bytes
//	Handshake:
//	  Type(1)=0x01 ClientHello
//	  Length(3)
//	  Version(2)
//	  Random(32)
//	  SessionID(1 + N)
//	  CipherSuites(2 + N)
//	  CompressionMethods(1 + N)
//	  Extensions(2 + N)
//	    foreach ext:
//	      Type(2) Length(2) Body...
//	      server_name ext (type=0):
//	        ListLen(2)
//	        NameType(1) NameLen(2) Name(N)
func extractSNI(r *bufio.Reader) (string, error) {
	hdr, err := r.Peek(5)
	if err != nil {
		return "", fmt.Errorf("peek record header: %w", err)
	}
	if hdr[0] != 0x16 {
		return "", fmt.Errorf("not a TLS handshake (content type 0x%x)", hdr[0])
	}
	recordLen := int(binary.BigEndian.Uint16(hdr[3:5]))
	if recordLen <= 0 || recordLen > tlsRecordMaxBytes {
		return "", fmt.Errorf("implausible record length %d", recordLen)
	}
	total := 5 + recordLen
	buf, err := r.Peek(total)
	if err != nil {
		return "", fmt.Errorf("peek full record: %w", err)
	}
	return parseClientHelloSNI(buf[5:total])
}

func parseClientHelloSNI(hs []byte) (string, error) {
	cur := 0
	if len(hs) < 4 || hs[cur] != 0x01 {
		return "", errors.New("not a ClientHello")
	}
	// skip type(1) + length(3)
	cur += 4
	// version(2) + random(32)
	cur += 2 + 32
	if cur > len(hs) {
		return "", errors.New("truncated pre-session")
	}
	// session id
	sidLen := int(hs[cur])
	cur += 1 + sidLen
	if cur+2 > len(hs) {
		return "", errors.New("truncated cipher_suites_len")
	}
	csLen := int(binary.BigEndian.Uint16(hs[cur : cur+2]))
	cur += 2 + csLen
	if cur+1 > len(hs) {
		return "", errors.New("truncated compression_methods_len")
	}
	cmLen := int(hs[cur])
	cur += 1 + cmLen
	if cur+2 > len(hs) {
		return "", errors.New("no extensions")
	}
	extLen := int(binary.BigEndian.Uint16(hs[cur : cur+2]))
	cur += 2
	end := cur + extLen
	if end > len(hs) {
		return "", errors.New("extensions length overruns")
	}
	for cur+4 <= end {
		extType := binary.BigEndian.Uint16(hs[cur : cur+2])
		cur += 2
		extLenInner := int(binary.BigEndian.Uint16(hs[cur : cur+2]))
		cur += 2
		if cur+extLenInner > end {
			return "", errors.New("extension body overruns")
		}
		body := hs[cur : cur+extLenInner]
		cur += extLenInner

		if extType != 0x00 { // server_name
			continue
		}
		return parseServerNameList(body)
	}
	return "", errors.New("no SNI extension present")
}

func parseServerNameList(body []byte) (string, error) {
	if len(body) < 2 {
		return "", errors.New("sni body too short")
	}
	listLen := int(binary.BigEndian.Uint16(body[0:2]))
	if 2+listLen > len(body) {
		return "", errors.New("sni list length overruns")
	}
	p := 2
	end := 2 + listLen
	for p+3 <= end {
		nameType := body[p]
		nameLen := int(binary.BigEndian.Uint16(body[p+1 : p+3]))
		p += 3
		if p+nameLen > end {
			return "", errors.New("sni name overruns")
		}
		if nameType == 0 { // host_name
			return string(body[p : p+nameLen]), nil
		}
		p += nameLen
	}
	return "", errors.New("no host_name in server_name list")
}
