// Package urlparse normalises whatever profile URL a caller sends into a
// vanity name.
package urlparse

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var vanityRE = regexp.MustCompile(`^[\p{L}\p{N}\-_.%]{1,120}$`)

// ErrInvalid is a malformed or unsupported profile URL.
type ErrInvalid struct{ Reason string }

func (e *ErrInvalid) Error() string { return e.Reason }

func invalid(format string, args ...any) error {
	return &ErrInvalid{Reason: fmt.Sprintf(format, args...)}
}

// ExtractVanity accepts a full URL, an /in/... path, or a bare vanity name.
func ExtractVanity(value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", invalid("profile url is required")
	}

	hasHost := strings.Contains(raw, "://") ||
		strings.HasPrefix(raw, "www.") || strings.HasPrefix(raw, "linkedin.com")

	path := raw
	if hasHost {
		candidate := raw
		if !strings.Contains(candidate, "://") {
			candidate = "https://" + candidate
		}
		parsed, err := url.Parse(candidate)
		if err != nil {
			return "", invalid("could not parse url: %v", err)
		}
		host := strings.ToLower(parsed.Hostname())
		// Regional subdomains (in.linkedin.com, uk.linkedin.com) are valid.
		if host != "linkedin.com" && !strings.HasSuffix(host, ".linkedin.com") {
			return "", invalid("not a linkedin.com url: %q", host)
		}
		path = parsed.Path
	}

	parts := make([]string, 0, 4)
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}

	index := -1
	for i, part := range parts {
		if part == "in" {
			index = i
			break
		}
	}

	var vanity string
	switch {
	case index >= 0:
		if index+1 >= len(parts) {
			return "", invalid("url has /in/ but no profile identifier")
		}
		vanity = parts[index+1]
	case hasHost:
		// A full linkedin.com URL must point at a member profile; without this
		// /feed/ and /jobs/ would parse as bare vanity names.
		return "", invalid("only member profile urls are supported (expected /in/<name>)")
	case len(parts) == 1:
		vanity = parts[0]
	case len(parts) == 0:
		return "", invalid("url has no profile path")
	default:
		return "", invalid("only member profile urls are supported (expected /in/<name>)")
	}

	decoded, err := url.PathUnescape(vanity)
	if err != nil {
		return "", invalid("invalid profile identifier: %q", vanity)
	}
	decoded = strings.TrimSpace(decoded)
	if decoded == "" || !vanityRE.MatchString(decoded) {
		return "", invalid("invalid profile identifier: %q", decoded)
	}
	return decoded, nil
}

// ProfileURL is the canonical URL for a vanity name.
func ProfileURL(vanity string) string {
	return "https://www.linkedin.com/in/" + vanity + "/"
}
