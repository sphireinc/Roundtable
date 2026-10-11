package egress

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGatewayRejectsUnlistedCONNECTWithoutDialing(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	dialed := false
	gateway := Gateway{
		Policy: Policy{AllowedHosts: []string{"api.example.com"}, Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		})},
		DialContext: func(context.Context, string, string) (net.Conn, error) { dialed = true; return nil, nil },
	}
	done := make(chan error, 1)
	go func() { done <- gateway.ServeConn(context.Background(), server) }()
	if _, err := io.WriteString(client, "CONNECT attacker.example:443 HTTP/1.1\r\nHost: attacker.example:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(client)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(response), "HTTP/1.1 403") || dialed {
		t.Fatalf("unlisted destination was not rejected before dialing: response=%q dialed=%v", response, dialed)
	}
	<-done
}

func TestGatewayPinsAllowedCONNECTAndTunnelsBytes(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	gateway := Gateway{
		Policy: Policy{AllowedHosts: []string{"api.example.com"}, Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		})},
		DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != "1.1.1.1:443" {
				t.Errorf("gateway did not dial the policy-pinned address: network=%s address=%s", network, address)
			}
			peer, tunnel := net.Pipe()
			go func() { _, _ = io.Copy(peer, peer); _ = peer.Close() }()
			return tunnel, nil
		},
	}
	done := make(chan error, 1)
	go func() { done <- gateway.ServeConn(context.Background(), server) }()
	if _, err := io.WriteString(client, "CONNECT api.example.com:443 HTTP/1.1\r\nHost: api.example.com:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if _, err := io.ReadFull(client, response); err != nil || string(response) != "HTTP/1.1 200 Connection Established\r\n\r\n" {
		t.Fatalf("CONNECT not established: response=%q err=%v", response, err)
	}
	clientHello := testTLSClientHello(t, "api.example.com")
	if _, err := client.Write(clientHello); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(clientHello))
	if _, err := io.ReadFull(client, got); err != nil || !bytes.Equal(got, clientHello) {
		t.Fatalf("tunnel did not preserve validated ClientHello: got=%x err=%v", got, err)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not stop after client disconnect")
	}
}

func TestGatewayRejectsClientHelloSNIThatDiffersFromAuthorizedConnectHost(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	dialed := false
	gateway := Gateway{
		Policy: Policy{AllowedHosts: []string{"api.example.com"}, Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		})},
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			_, upstream := net.Pipe()
			return upstream, nil
		},
	}
	done := make(chan error, 1)
	go func() { done <- gateway.ServeConn(context.Background(), server) }()
	_, _ = io.WriteString(client, "CONNECT api.example.com:443 HTTP/1.1\r\nHost: api.example.com:443\r\n\r\n")
	response := make([]byte, len("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if _, err := io.ReadFull(client, response); err != nil || !bytes.HasPrefix(response, []byte("HTTP/1.1 200")) {
		t.Fatalf("CONNECT did not establish before TLS inspection: %q %v", response, err)
	}
	if _, err := client.Write(testTLSClientHello(t, "other.example.com")); err != nil {
		t.Fatal(err)
	}
	denial, err := io.ReadAll(client)
	if err != nil || !bytes.HasPrefix(denial, []byte("HTTP/1.1 403")) {
		t.Fatalf("mismatched SNI was not explicitly denied: %q %v", denial, err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not reject mismatched TLS SNI")
	}
	if dialed {
		t.Fatal("gateway dialed an upstream after mismatched TLS SNI")
	}
}

func testTLSClientHello(t *testing.T, serverName string) []byte {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = tls.Client(client, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}).Handshake()
	}()
	var header [5]byte
	if _, err := io.ReadFull(server, header[:]); err != nil {
		t.Fatal(err)
	}
	length := int(header[3])<<8 | int(header[4])
	record := append([]byte(nil), header[:]...)
	body := make([]byte, length)
	if _, err := io.ReadFull(server, body); err != nil {
		t.Fatal(err)
	}
	record = append(record, body...)
	_ = client.Close()
	_ = server.Close()
	<-done
	return record
}

func TestGatewayCancellationClosesClientStalledBeforeHeaders(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- (Gateway{}).ServeConn(ctx, server) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not close a stalled client after cancellation")
	}
}

func TestGatewayBoundsSessionAndGlobalConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	globalSlots := make(chan struct{}, 2)
	listeners := []*queuedConnListener{newQueuedConnListener(), newQueuedConnListener()}
	done := make([]chan error, len(listeners))
	for i, listener := range listeners {
		done[i] = make(chan error, 1)
		gateway := Gateway{maxConnections: 2, globalSlots: globalSlots, headerTimeout: time.Second}
		go func() { done[i] <- gateway.Serve(ctx, listener) }()
	}
	clients := make([]net.Conn, 0, 4)
	for i := 0; i < 3; i++ {
		client, server := net.Pipe()
		clients = append(clients, client)
		listeners[i%len(listeners)].queue <- server
	}
	for _, listener := range listeners {
		want := 1
		if listener == listeners[0] {
			want = 2
		}
		for i := 0; i < want; i++ {
			select {
			case <-listener.accepted:
			case <-time.After(time.Second):
				t.Fatal("gateway did not accept queued test connection")
			}
		}
	}
	closed := 0
	for _, client := range clients {
		_ = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		var one [1]byte
		_, err := client.Read(one[:])
		if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
			closed++
		}
	}
	if closed != 1 || len(globalSlots) != 2 {
		t.Fatalf("gateway did not cap host-side handlers: closed=%d active_global=%d", closed, len(globalSlots))
	}
	cancel()
	for _, listener := range listeners {
		_ = listener.Close()
	}
	for _, result := range done {
		select {
		case err := <-result:
			if err != nil {
				t.Errorf("gateway shutdown: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("gateway did not release active connections after cancellation")
		}
	}
	for _, client := range clients {
		_ = client.Close()
	}
}

func TestGatewayBoundsConnectionsPerSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	listener := newQueuedConnListener()
	globalSlots := make(chan struct{}, 3)
	done := make(chan error, 1)
	go func() {
		done <- (Gateway{maxConnections: 2, globalSlots: globalSlots, headerTimeout: time.Second}).Serve(ctx, listener)
	}()
	clients := make([]net.Conn, 0, 3)
	for i := 0; i < 3; i++ {
		client, server := net.Pipe()
		clients = append(clients, client)
		listener.queue <- server
	}
	for i := 0; i < 3; i++ {
		select {
		case <-listener.accepted:
		case <-time.After(time.Second):
			t.Fatal("gateway did not accept queued test connection")
		}
	}
	closed := 0
	for _, client := range clients {
		_ = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		var one [1]byte
		_, err := client.Read(one[:])
		if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
			closed++
		}
	}
	if closed != 1 || len(globalSlots) != 2 {
		t.Fatalf("per-session egress limit did not reject the excess connection: closed=%d active=%d", closed, len(globalSlots))
	}
	cancel()
	_ = listener.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("gateway did not stop after cancellation")
	}
	if len(globalSlots) != 0 {
		t.Fatalf("gateway leaked global egress slots after cancellation: %d", len(globalSlots))
	}
	for _, client := range clients {
		_ = client.Close()
	}
}

func TestGatewayAppliesDeadlineToIncompleteCONNECTHeaders(t *testing.T) {
	client, server := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- (Gateway{headerTimeout: 20 * time.Millisecond}).ServeConn(context.Background(), server)
	}()
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	response, err := io.ReadAll(client)
	if err != nil || !strings.HasPrefix(string(response), "HTTP/1.1 400") {
		t.Fatalf("incomplete CONNECT header was not closed after its deadline: response=%q err=%v", response, err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway retained an incomplete CONNECT handler after its deadline")
	}
}

func TestEstablishedTunnelCancellationClosesBothTCPConnections(t *testing.T) {
	upstreamListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("host sandbox does not permit loopback TCP listeners: %v", err)
	}
	defer upstreamListener.Close()
	upstreamAccepted := make(chan net.Conn, 1)
	go func() {
		conn, err := upstreamListener.Accept()
		if err == nil {
			upstreamAccepted <- conn
		}
	}()
	clientSide, err := net.Dial("tcp", upstreamListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer clientSide.Close()
	var upstream net.Conn
	select {
	case upstream = <-upstreamAccepted:
	case <-time.After(time.Second):
		t.Fatal("upstream connection was not accepted")
	}
	defer upstream.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tunnel(ctx, clientSide, clientSide, upstream, upstream) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled established tunnel retained a half-closed connection")
	}
}

type queuedConnListener struct {
	queue    chan net.Conn
	accepted chan struct{}
	closed   chan struct{}
	once     sync.Once
}

func newQueuedConnListener() *queuedConnListener {
	return &queuedConnListener{queue: make(chan net.Conn, 4), accepted: make(chan struct{}, 4), closed: make(chan struct{})}
}

func (l *queuedConnListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.queue:
		l.accepted <- struct{}{}
		return conn, nil
	case <-l.closed:
		return nil, errors.New("listener closed")
	}
}

func (l *queuedConnListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *queuedConnListener) Addr() net.Addr { return testAddr("queued") }

type testAddr string

func (a testAddr) Network() string { return string(a) }
func (a testAddr) String() string  { return string(a) }
