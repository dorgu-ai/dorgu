package generator

import (
	"regexp"
	"strings"
)

const maxDNSSubdomainLen = 253

// ToDNSSubdomain converts an app name to a valid Kubernetes DNS subdomain (RFC 1123).
// Replaces underscores with hyphens, lowercases, and ensures alphanumeric start/end.
func ToDNSSubdomain(name string) string {
	if name == "" {
		return "app"
	}
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, "_", "-")
	// Strip characters not in [a-z0-9\-.]
	allowed := regexp.MustCompile(`[^a-z0-9\-.]`)
	s = allowed.ReplaceAllString(s, "")
	// Collapse multiple hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-.")
	if s == "" {
		return "app"
	}
	if len(s) > maxDNSSubdomainLen {
		s = s[:maxDNSSubdomainLen]
		s = strings.TrimRight(s, "-.")
		if s == "" {
			return "app"
		}
	}
	return s
}

// MapResourceProfileToCRD maps CLI/config profile (api, worker, web) to the
// ApplicationPersona CRD enum (minimal, standard, compute-heavy, memory-heavy).
func MapResourceProfileToCRD(profile string) string {
	switch profile {
	case "api":
		return "standard"
	case "worker":
		return "compute-heavy"
	case "web":
		return "minimal"
	default:
		return "standard"
	}
}
