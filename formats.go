package swaggerkit

import (
	"net/mail"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	uuidPattern     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	hostnameLabel   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	base64Pattern   = regexp.MustCompile(`^[A-Za-z0-9+/]*={0,2}$`)
	emailDomainPart = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)
)

// checkFormat returns an error message if s does not match the string format.
// Unknown formats are not checked, as JSON Schema requires.
func checkFormat(format, s string) string {
	switch format {
	case "email":
		if !isEmail(s) {
			return "must be a valid email address"
		}
	case "uri":
		u, err := url.Parse(s)
		if err != nil || u.Scheme == "" || (u.Host == "" && u.Opaque == "") {
			return "must be an absolute URL"
		}
	case "uuid":
		if !uuidPattern.MatchString(s) {
			return "must be a UUID"
		}
	case "ipv4":
		if a, err := netip.ParseAddr(s); err != nil || !a.Is4() {
			return "must be an IPv4 address"
		}
	case "ipv6":
		if a, err := netip.ParseAddr(s); err != nil || !a.Is6() {
			return "must be an IPv6 address"
		}
	case "hostname":
		if !isHostname(s) {
			return "must be a hostname"
		}
	case "date":
		if _, err := time.Parse(time.DateOnly, s); err != nil {
			return "must be a date in YYYY-MM-DD format"
		}
	case "date-time":
		if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
			return "must be a date-time in RFC 3339 format"
		}
	case "byte":
		if len(s)%4 != 0 || !base64Pattern.MatchString(s) {
			return "must be base64 encoded"
		}
	}
	return ""
}

func isEmail(s string) bool {
	if len(s) > 254 {
		return false
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || addr.Name != "" {
		return false // reject "Name <a@b.c>" forms
	}
	at := strings.LastIndexByte(s, '@')
	domain := s[at+1:]
	return at > 0 && at <= 64 && strings.Contains(domain, ".") && emailDomainPart.MatchString(domain) && isHostname(domain)
}

func isHostname(s string) bool {
	s = strings.TrimSuffix(s, ".")
	if s == "" || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if !hostnameLabel.MatchString(label) {
			return false
		}
	}
	return true
}
