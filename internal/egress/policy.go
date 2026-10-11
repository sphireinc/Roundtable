package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type Policy struct {
	AllowedHosts []string
	Resolver     Resolver
}

type Target struct {
	Host    string
	Port    string
	Address netip.Addr
}

func ValidateHostname(host string) error {
	_, err := normalizeHostname(host)
	if err != nil || net.ParseIP(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")) != nil {
		return errors.New("provider hostname must be an exact ASCII DNS name, not an IP or wildcard")
	}
	return nil
}

func (p Policy) Authorize(ctx context.Context, authority string) (Target, error) {
	host, port, err := net.SplitHostPort(authority)
	if err != nil || port != "443" || host == "" {
		return Target{}, errors.New("provider egress permits only hostname:443 CONNECT targets")
	}
	host, err = normalizeHostname(host)
	if err != nil {
		return Target{}, err
	}
	if net.ParseIP(host) != nil {
		return Target{}, errors.New("provider egress does not permit IP-literal destinations")
	}
	allowed := false
	for _, candidate := range p.AllowedHosts {
		candidate, err = normalizeHostname(candidate)
		if err != nil {
			return Target{}, errors.New("provider egress policy contains an invalid hostname")
		}
		if host == candidate {
			allowed = true
			break
		}
	}
	if !allowed {
		return Target{}, fmt.Errorf("provider egress hostname %q is not allowlisted", host)
	}
	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return Target{}, fmt.Errorf("resolve allowlisted provider hostname %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return Target{}, fmt.Errorf("resolve allowlisted provider hostname %q: no addresses returned", host)
	}
	for _, address := range addresses {
		address = address.Unmap()
		if !publicUnicast(address) {
			return Target{}, fmt.Errorf("provider hostname %q resolved to a non-public address", host)
		}
	}
	return Target{Host: host, Port: port, Address: addresses[0].Unmap()}, nil
}

func normalizeHostname(host string) (string, error) {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if len(host) == 0 || len(host) > 253 {
		return "", errors.New("provider egress hostname is invalid")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("provider egress hostname is invalid")
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return "", errors.New("provider egress hostname must be an ASCII DNS name")
			}
		}
	}
	return host, nil
}

func publicUnicast(address netip.Addr) bool {
	// Deny non-public ranges and conservative special-use prefixes from the IANA registries.
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	var blocked []netip.Prefix
	if address.Is4() {
		blocked = []netip.Prefix{
			netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
			netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
			netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
			netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
			netip.MustParsePrefix("240.0.0.0/4"),
		}
	} else if address.Is6() {
		if !netip.MustParsePrefix("2000::/3").Contains(address) {
			return false
		}
		blocked = []netip.Prefix{
			netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
			netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
		}
	} else {
		return false
	}
	for _, prefix := range blocked {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
