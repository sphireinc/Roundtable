package egress

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxConnectHeaderBytes = 16 << 10
const defaultMaxSessionConnections = 32
const defaultMaxGlobalConnections = 256
const defaultHeaderReadTimeout = 5 * time.Second
const maxTLSClientHelloBytes = 64 << 10

var globalProviderEgressSlots = make(chan struct{}, defaultMaxGlobalConnections)

type Gateway struct {
	Policy         Policy
	DialContext    func(context.Context, string, string) (net.Conn, error)
	maxConnections int
	globalSlots    chan struct{}
	headerTimeout  time.Duration
}

func (g Gateway) Serve(ctx context.Context, listener net.Listener) error {
	if listener == nil {
		return errors.New("provider egress listener is required")
	}
	maxConnections := g.maxConnections
	if maxConnections <= 0 {
		maxConnections = defaultMaxSessionConnections
	}
	globalSlots := g.globalSlots
	if globalSlots == nil {
		globalSlots = globalProviderEgressSlots
	}
	sessionSlots := make(chan struct{}, maxConnections)
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	var active sync.WaitGroup
	for {
		conn, err := listener.Accept()
		if err != nil {
			active.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept provider egress connection: %w", err)
		}
		select {
		case sessionSlots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		select {
		case globalSlots <- struct{}{}:
		default:
			<-sessionSlots
			_ = conn.Close()
			continue
		}
		active.Add(1)
		go func() {
			defer active.Done()
			defer func() { <-sessionSlots; <-globalSlots }()
			_ = g.ServeConn(ctx, conn)
		}()
	}
}

func (g Gateway) ServeConn(ctx context.Context, client net.Conn) error {
	if client == nil {
		return errors.New("provider egress client connection is required")
	}
	connectionDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
		case <-connectionDone:
		}
	}()
	defer close(connectionDone)
	defer client.Close()
	headerTimeout := g.headerTimeout
	if headerTimeout <= 0 {
		headerTimeout = defaultHeaderReadTimeout
	}
	if err := client.SetReadDeadline(time.Now().Add(headerTimeout)); err != nil {
		return fmt.Errorf("set provider CONNECT header deadline: %w", err)
	}
	reader := bufio.NewReaderSize(client, 4096)
	headerLimit := &io.LimitedReader{R: reader, N: maxConnectHeaderBytes}
	limitedReader := bufio.NewReaderSize(headerLimit, 4096)
	request, err := http.ReadRequest(limitedReader)
	if err != nil {
		writeProxyStatus(client, http.StatusBadRequest, "Bad Request")
		return fmt.Errorf("read provider CONNECT request: %w", err)
	}
	if request.Body != nil {
		_ = request.Body.Close()
	}
	if err := client.SetReadDeadline(time.Time{}); err != nil {
		return fmt.Errorf("clear provider CONNECT header deadline: %w", err)
	}
	if headerLimit.N == 0 || request.Method != http.MethodConnect || request.Host == "" || !strings.EqualFold(request.RequestURI, request.Host) || request.ContentLength > 0 || len(request.TransferEncoding) != 0 || request.Header.Get("Proxy-Authorization") != "" {
		writeProxyStatus(client, http.StatusForbidden, "Forbidden")
		return errors.New("provider egress accepts only bodyless CONNECT requests without proxy credentials")
	}
	headerLimit.N = int64(^uint64(0) >> 1)
	target, err := g.Policy.Authorize(ctx, request.Host)
	if err != nil {
		writeProxyStatus(client, http.StatusForbidden, "Forbidden")
		return err
	}
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return fmt.Errorf("establish provider CONNECT tunnel: %w", err)
	}
	if err := client.SetReadDeadline(time.Now().Add(headerTimeout)); err != nil {
		return fmt.Errorf("set provider TLS ClientHello deadline: %w", err)
	}
	clientHello, serverName, err := readProviderClientHello(limitedReader)
	if err != nil {
		writeProxyStatus(client, http.StatusForbidden, "Forbidden")
		return fmt.Errorf("read provider TLS ClientHello: %w", err)
	}
	if serverName != target.Host {
		writeProxyStatus(client, http.StatusForbidden, "Forbidden")
		return fmt.Errorf("provider TLS SNI %q does not match authorized CONNECT host %q", serverName, target.Host)
	}
	if err := client.SetReadDeadline(time.Time{}); err != nil {
		return fmt.Errorf("clear provider TLS ClientHello deadline: %w", err)
	}
	dial := g.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	upstream, err := dial(ctx, "tcp", net.JoinHostPort(target.Address.String(), target.Port))
	if err != nil {
		writeProxyStatus(client, http.StatusBadGateway, "Bad Gateway")
		return fmt.Errorf("connect to authorized provider endpoint: %w", err)
	}
	defer upstream.Close()
	return tunnel(ctx, client, io.MultiReader(bytes.NewReader(clientHello), limitedReader), upstream, upstream)
}

func readProviderClientHello(reader io.Reader) ([]byte, string, error) {
	var wire, handshake []byte
	for len(handshake) < 4 || len(handshake) < 4+int(handshakeLength(handshake)) {
		var header [5]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			return nil, "", err
		}
		if header[0] != 22 || header[1] != 3 || header[2] > 4 {
			return nil, "", errors.New("expected a TLS handshake record")
		}
		recordLength := int(header[3])<<8 | int(header[4])
		if recordLength == 0 || len(wire)+5+recordLength > maxTLSClientHelloBytes {
			return nil, "", errors.New("TLS ClientHello exceeds the size limit")
		}
		record := make([]byte, recordLength)
		if _, err := io.ReadFull(reader, record); err != nil {
			return nil, "", err
		}
		wire = append(wire, header[:]...)
		wire = append(wire, record...)
		handshake = append(handshake, record...)
		if len(handshake) >= 4 && handshake[0] != 1 {
			return nil, "", errors.New("first TLS handshake message is not ClientHello")
		}
	}
	name, err := parseProviderSNI(handshake[4 : 4+int(handshakeLength(handshake))])
	if err != nil {
		return nil, "", err
	}
	return wire, name, nil
}

func handshakeLength(handshake []byte) uint32 {
	if len(handshake) < 4 {
		return 0
	}
	return uint32(handshake[1])<<16 | uint32(handshake[2])<<8 | uint32(handshake[3])
}

func parseProviderSNI(body []byte) (string, error) {
	readVector := func(offset *int, width int) ([]byte, error) {
		if *offset+width > len(body) {
			return nil, errors.New("truncated TLS ClientHello vector")
		}
		length := 0
		for _, b := range body[*offset : *offset+width] {
			length = length<<8 | int(b)
		}
		*offset += width
		if length > len(body)-*offset {
			return nil, errors.New("invalid TLS ClientHello vector length")
		}
		value := body[*offset : *offset+length]
		*offset += length
		return value, nil
	}
	offset := 34                                      // legacy_version and random
	if _, err := readVector(&offset, 1); err != nil { // session id
		return "", err
	}
	if _, err := readVector(&offset, 2); err != nil { // cipher suites
		return "", err
	}
	if _, err := readVector(&offset, 1); err != nil { // compression methods
		return "", err
	}
	extensions, err := readVector(&offset, 2)
	if err != nil || offset != len(body) {
		return "", errors.New("invalid TLS ClientHello extensions")
	}
	var serverName string
	for offset := 0; offset < len(extensions); {
		if offset+4 > len(extensions) {
			return "", errors.New("truncated TLS extension")
		}
		typeID := uint16(extensions[offset])<<8 | uint16(extensions[offset+1])
		offset += 2
		length := int(extensions[offset])<<8 | int(extensions[offset+1])
		offset += 2
		if length > len(extensions)-offset {
			return "", errors.New("invalid TLS extension length")
		}
		value := extensions[offset : offset+length]
		offset += length
		if typeID == 0xfe0d {
			return "", errors.New("encrypted client hello is not permitted through provider egress")
		}
		if typeID != 0 {
			continue
		}
		nameOffset := 0
		list, err := func() ([]byte, error) {
			if len(value) < 2 {
				return nil, errors.New("truncated TLS server name list")
			}
			length := int(value[0])<<8 | int(value[1])
			if length != len(value)-2 {
				return nil, errors.New("invalid TLS server name list length")
			}
			return value[2:], nil
		}()
		if err != nil {
			return "", err
		}
		for nameOffset < len(list) {
			if nameOffset+3 > len(list) {
				return "", errors.New("truncated TLS server name")
			}
			kind := list[nameOffset]
			nameLength := int(list[nameOffset+1])<<8 | int(list[nameOffset+2])
			nameOffset += 3
			if nameLength > len(list)-nameOffset {
				return "", errors.New("invalid TLS server name length")
			}
			if kind == 0 {
				if serverName != "" {
					return "", errors.New("duplicate TLS host_name values")
				}
				serverName = strings.ToLower(strings.TrimSuffix(string(list[nameOffset:nameOffset+nameLength]), "."))
			}
			nameOffset += nameLength
		}
	}
	if serverName == "" {
		return "", errors.New("TLS ClientHello must include a visible host_name SNI")
	}
	return serverName, nil
}

func tunnel(ctx context.Context, left net.Conn, leftReader io.Reader, right net.Conn, rightReader io.Reader) error {
	tunnelDone := make(chan struct{})
	defer close(tunnelDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = left.Close()
			_ = right.Close()
		case <-tunnelDone:
		}
	}()
	done := make(chan error, 2)
	copySide := func(destination net.Conn, source io.Reader) {
		_, err := io.Copy(destination, source)
		if closer, ok := destination.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		}
		done <- err
	}
	go copySide(right, leftReader)
	go copySide(left, rightReader)
	firstErr := <-done
	_, leftHalfClose := left.(interface{ CloseWrite() error })
	_, rightHalfClose := right.(interface{ CloseWrite() error })
	if !leftHalfClose || !rightHalfClose || firstErr != nil && !errors.Is(firstErr, net.ErrClosed) {
		_ = left.Close()
		_ = right.Close()
	}
	secondErr := <-done
	if firstErr != nil && !errors.Is(firstErr, net.ErrClosed) {
		return firstErr
	}
	if secondErr != nil && !errors.Is(secondErr, net.ErrClosed) {
		return secondErr
	}
	return nil
}

func writeProxyStatus(conn net.Conn, status int, text string) {
	_, _ = fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nConnection: close\r\nContent-Length: 0\r\n\r\n", status, text)
}
