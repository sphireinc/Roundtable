package egress

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestLocalProxyForwardsCONNECTOnlyThroughBrokerSocket(t *testing.T) {
	client, proxy := net.Pipe()
	brokerClient, brokerServer := net.Pipe()
	defer client.Close()
	gateway := Gateway{
		Policy: Policy{AllowedHosts: []string{"api.example.com"}, Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		})},
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			provider, agent := net.Pipe()
			go func() { _, _ = io.Copy(provider, provider); _ = provider.Close() }()
			return agent, nil
		},
	}
	brokerDone := make(chan error, 1)
	go func() { brokerDone <- gateway.ServeConn(context.Background(), brokerServer) }()
	proxyDone := make(chan error, 1)
	go func() {
		proxyDone <- serveLocalProxyConn(context.Background(), proxy, func(context.Context) (net.Conn, error) { return brokerClient, nil })
	}()
	if _, err := io.WriteString(client, "CONNECT api.example.com:443 HTTP/1.1\r\nHost: api.example.com:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if _, err := io.ReadFull(client, response); err != nil || string(response) != "HTTP/1.1 200 Connection Established\r\n\r\n" {
		t.Fatalf("proxy did not establish broker tunnel: response=%q err=%v", response, err)
	}
	clientHello := testTLSClientHello(t, "api.example.com")
	if _, err := client.Write(clientHello); err != nil {
		t.Fatal(err)
	}
	echo := make([]byte, len(clientHello))
	if _, err := io.ReadFull(client, echo); err != nil || !bytes.Equal(echo, clientHello) {
		t.Fatalf("broker tunnel did not carry provider bytes: echo=%q err=%v", echo, err)
	}
	_ = client.Close()
	select {
	case <-proxyDone:
	case <-time.After(time.Second):
		t.Fatal("local proxy did not stop after client disconnect")
	}
	select {
	case <-brokerDone:
	case <-time.After(time.Second):
		t.Fatal("provider broker did not stop after local proxy disconnect")
	}
}

func TestLocalProxyRejectsNonConnectBeforeUsingBroker(t *testing.T) {
	client, proxy := net.Pipe()
	defer client.Close()
	brokerDialed := false
	proxyDone := make(chan error, 1)
	go func() {
		proxyDone <- serveLocalProxyConn(context.Background(), proxy, func(context.Context) (net.Conn, error) {
			brokerDialed = true
			return nil, nil
		})
	}()
	if _, err := io.WriteString(client, "GET http://attacker.example/ HTTP/1.1\r\nHost: attacker.example\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(client)
	if err != nil || !strings.HasPrefix(string(response), "HTTP/1.1 403") || brokerDialed {
		t.Fatalf("non-CONNECT proxy request was not denied locally: response=%q brokerDialed=%v err=%v", response, brokerDialed, err)
	}
	<-proxyDone
}
