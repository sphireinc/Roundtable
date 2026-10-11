package security

import (
	"path/filepath"
	"strings"
)

// ValidAgentPath validates one operator-authorized relative repository path.
func ValidAgentPath(value string) bool {
	clean := filepath.ToSlash(filepath.Clean(value))
	return value != "" && clean == value && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && !filepath.IsAbs(value) && !SensitiveAgentPath(clean)
}

// SensitiveAgentPath denies common credential, key, and local configuration paths.
func SensitiveAgentPath(value string) bool {
	parts := strings.Split(strings.ToLower(filepath.ToSlash(value)), "/")
	for _, part := range parts {
		if part == ".git" || part == ".roundtable" || part == ".ssh" || part == ".aws" || part == ".kube" || part == ".config" || part == ".docker" || part == ".dockerconfigjson" || part == ".env" || strings.HasPrefix(part, ".env.") || part == ".netrc" || part == ".npmrc" || part == ".pypirc" || part == "authorized_keys" || part == "known_hosts" || part == "service-account.json" {
			return true
		}
		if strings.Contains(part, "credential") || strings.Contains(part, "secret") || strings.Contains(part, "private") || strings.HasPrefix(part, "id_rsa") || strings.HasPrefix(part, "id_ecdsa") || strings.HasPrefix(part, "id_ed25519") || strings.HasPrefix(part, "id_dsa") || strings.HasSuffix(part, ".pem") || strings.HasSuffix(part, ".key") || strings.HasSuffix(part, ".p12") || strings.HasSuffix(part, ".pfx") {
			return true
		}
	}
	return false
}
