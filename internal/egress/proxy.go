package egress

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type BrokerDial func(context.Context) (net.Conn, error)

func serveLocalProxyConn(ctx context.Context, client net.Conn, dial BrokerDial) error {
	if client == nil || dial == nil {
		return errors.New("local provider proxy requires a client and broker dialer")
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
	reader := bufio.NewReaderSize(client, 4096)
	limited := &io.LimitedReader{R: reader, N: maxConnectHeaderBytes}
	limitedReader := bufio.NewReaderSize(limited, 4096)
	request, err := http.ReadRequest(limitedReader)
	if err != nil {
		writeProxyStatus(client, http.StatusBadRequest, "Bad Request")
		return fmt.Errorf("read local provider proxy request: %w", err)
	}
	if request.Body != nil {
		_ = request.Body.Close()
	}
	host, port, authorityErr := net.SplitHostPort(request.Host)
	if limited.N == 0 || request.Method != http.MethodConnect || authorityErr != nil || host == "" || port != "443" || !strings.EqualFold(request.RequestURI, request.Host) || request.ContentLength > 0 || len(request.TransferEncoding) != 0 || request.Header.Get("Proxy-Authorization") != "" {
		writeProxyStatus(client, http.StatusForbidden, "Forbidden")
		return errors.New("local provider proxy accepts only bodyless CONNECT requests for port 443")
	}
	limited.N = int64(^uint64(0) >> 1)
	broker, err := dial(ctx)
	if err != nil {
		writeProxyStatus(client, http.StatusBadGateway, "Bad Gateway")
		return fmt.Errorf("connect to session provider egress broker: %w", err)
	}
	defer broker.Close()
	if _, err := fmt.Fprintf(broker, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", request.Host, request.Host); err != nil {
		writeProxyStatus(client, http.StatusBadGateway, "Bad Gateway")
		return fmt.Errorf("request provider egress authorization: %w", err)
	}
	brokerReader := bufio.NewReaderSize(broker, 4096)
	response, err := http.ReadResponse(brokerReader, request)
	if err != nil {
		writeProxyStatus(client, http.StatusBadGateway, "Bad Gateway")
		return fmt.Errorf("read provider egress authorization response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		statusText := http.StatusText(response.StatusCode)
		if statusText == "" {
			statusText = "Forbidden"
		}
		writeProxyStatus(client, response.StatusCode, statusText)
		return fmt.Errorf("provider egress broker denied CONNECT with status %d", response.StatusCode)
	}
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return fmt.Errorf("establish local provider proxy tunnel: %w", err)
	}
	return tunnel(ctx, client, limitedReader, broker, brokerReader)
}

func ServeLocalProxy(ctx context.Context, listener net.Listener, brokerSocket string) error {
	if listener == nil || !filepath.IsAbs(brokerSocket) || filepath.Clean(brokerSocket) != brokerSocket {
		return errors.New("local provider proxy requires a listener and clean broker socket path")
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	var active sync.WaitGroup
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			active.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept local provider proxy connection: %w", err)
		}
		active.Add(1)
		go func() {
			defer active.Done()
			_ = serveLocalProxyConn(ctx, conn, func(ctx context.Context) (net.Conn, error) {
				return dialer.DialContext(ctx, "unix", brokerSocket)
			})
		}()
	}
}
