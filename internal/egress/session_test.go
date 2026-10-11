package egress

import (
	"context"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestSessionServesOnlyItsPolicyAndRemovesSocketOnClose(t *testing.T) {
	dialed := false
	session, err := listenTestSession(t, context.Background(), Policy{
		AllowedHosts: []string{"api.example.com"},
		Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		}),
	}, func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	path := session.SocketPath()
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("connect to session egress socket: %v", err)
	}
	if _, err := io.WriteString(client, "CONNECT attacker.example:443 HTTP/1.1\r\nHost: attacker.example:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(client)
	_ = client.Close()
	if err != nil || !strings.HasPrefix(string(response), "HTTP/1.1 403") || dialed {
		t.Fatalf("session gateway did not enforce its assigned policy: response=%q dialed=%v err=%v", response, dialed, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := net.DialTimeout("unix", path, 100*time.Millisecond); err == nil {
		t.Fatal("session egress socket remained available after close")
	}
}

func TestSessionCloseTerminatesStalledGatewayConnections(t *testing.T) {
	session, err := listenTestSession(t, context.Background(), Policy{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := net.Dial("unix", session.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	closed := make(chan error, 1)
	go func() { closed <- session.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("session close waited indefinitely on an untrusted stalled client")
	}
}

func listenTestSession(t *testing.T, ctx context.Context, policy Policy, dial DialContextFunc) (*Session, error) {
	t.Helper()
	session, err := ListenSession(ctx, policy, dial)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
		t.Skipf("Unix socket binding is unavailable in this host sandbox: %v", err)
	}
	return session, err
}
