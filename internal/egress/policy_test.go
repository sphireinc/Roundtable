package egress

import (
	"context"
	"net/netip"
	"testing"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

func TestPolicyAuthorizesExactProviderHostAndPublicResolvedAddress(t *testing.T) {
	policy := Policy{
		AllowedHosts: []string{"api.example.com"},
		Resolver: resolverFunc(func(_ context.Context, network, host string) ([]netip.Addr, error) {
			if network != "ip" || host != "api.example.com" {
				t.Fatalf("unexpected DNS query network=%q host=%q", network, host)
			}
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		}),
	}
	target, err := policy.Authorize(context.Background(), "api.example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	if target.Host != "api.example.com" || target.Port != "443" || target.Address != netip.MustParseAddr("1.1.1.1") {
		t.Fatalf("unexpected authorized target: %+v", target)
	}
}

func TestPolicyRejectsUnlistedHostAndUnsafeResolvedAddresses(t *testing.T) {
	for _, test := range []struct {
		name string
		host string
		ips  []netip.Addr
	}{
		{name: "unlisted hostname", host: "attacker.example", ips: []netip.Addr{netip.MustParseAddr("1.1.1.1")}},
		{name: "private address", host: "api.example.com", ips: []netip.Addr{netip.MustParseAddr("10.0.0.8")}},
		{name: "mixed public and loopback answer", host: "api.example.com", ips: []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("127.0.0.1")}},
		{name: "IPv6 documentation range", host: "api.example.com", ips: []netip.Addr{netip.MustParseAddr("3fff::1")}},
		{name: "shared carrier-grade NAT range", host: "api.example.com", ips: []netip.Addr{netip.MustParseAddr("100.64.0.1")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := Policy{AllowedHosts: []string{"api.example.com"}, Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return test.ips, nil })}
			if _, err := policy.Authorize(context.Background(), test.host+":443"); err == nil {
				t.Fatal("unsafe provider egress target was authorized")
			}
		})
	}
}

func TestPolicyRejectsNonTLSAndMalformedConnectTargets(t *testing.T) {
	policy := Policy{AllowedHosts: []string{"api.example.com"}, Resolver: resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	})}
	for _, target := range []string{"api.example.com:80", "api.example.com:444", "127.0.0.1:443", "api.example.com:443/path", "api.example.com:", "user@api.example.com:443"} {
		if _, err := policy.Authorize(context.Background(), target); err == nil {
			t.Errorf("unsafe CONNECT target %q was authorized", target)
		}
	}
}
