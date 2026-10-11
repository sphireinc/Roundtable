package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func RunAgent(ctx context.Context, brokerSocket string, command []string, stdout, stderr io.Writer) error {
	if !filepath.IsAbs(brokerSocket) || filepath.Clean(brokerSocket) != brokerSocket {
		return errors.New("agent execution requires an absolute clean provider egress Unix socket path")
	}
	info, err := os.Lstat(brokerSocket)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return errors.New("agent provider egress broker Unix socket is unavailable")
	}
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return errors.New("agent provider command is required")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start loopback-only provider proxy: %w", err)
	}
	proxyCtx, cancelProxy := context.WithCancel(ctx)
	proxyDone := make(chan error, 1)
	go func() { proxyDone <- ServeLocalProxy(proxyCtx, listener, brokerSocket) }()
	proxyURL := "http://" + listener.Addr().String()
	child := exec.CommandContext(ctx, command[0], command[1:]...)
	child.Env = proxyEnvironment(os.Environ(), proxyURL)
	child.Stdout, child.Stderr = stdout, stderr
	child.Stdin = os.Stdin
	commandErr := child.Run()
	cancelProxy()
	proxyErr := <-proxyDone
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if commandErr != nil {
		return commandErr
	}
	if proxyErr != nil {
		return fmt.Errorf("stop loopback-only provider proxy: %w", proxyErr)
	}
	return nil
}

func proxyEnvironment(environment []string, proxyURL string) []string {
	proxyNames := map[string]bool{
		"http_proxy": true, "https_proxy": true, "all_proxy": true,
		"no_proxy": true, "ftp_proxy": true,
	}
	result := make([]string, 0, len(environment)+8)
	for _, entry := range environment {
		name, _, ok := strings.Cut(entry, "=")
		if ok && proxyNames[strings.ToLower(name)] {
			continue
		}
		result = append(result, entry)
	}
	result = append(result,
		"HTTP_PROXY="+proxyURL, "HTTPS_PROXY="+proxyURL, "ALL_PROXY="+proxyURL,
		"http_proxy="+proxyURL, "https_proxy="+proxyURL, "all_proxy="+proxyURL,
		"NO_PROXY=localhost,127.0.0.1,::1", "no_proxy=localhost,127.0.0.1,::1",
	)
	return result
}
