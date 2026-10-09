package lists

import (
	"net"
	"regexp"
	"strings"
)

// Entries is a parsed list, one slice per rule kind.
type Entries struct {
	Suffix  []string // the domain and all its subdomains
	Exact   []string // exactly this domain
	Keyword []string // any domain containing the word
	Regex   []string // regular expressions (RE2)
	CIDR    []string
}

// Empty reports whether nothing was parsed.
func (e Entries) Empty() bool {
	return len(e.Suffix)+len(e.Exact)+len(e.Keyword)+len(e.Regex)+len(e.CIDR) == 0
}

// ParseEntries parses a plain-text list. One entry per line; "#" and "//"
// start comments. An entry is one of:
//
//	example.com, .example.com, *.example.com, namespace:example.com,
//	domain:example.com, suffix:example.com   the domain and its subdomains
//	full:example.com, exact:example.com      exactly this domain
//	keyword:word                             any domain containing the word
//	wildcard:cdn*.example.?om, cdn*.a.com    "*" any characters, "?" one
//	regexp:^ads?\..*, regex:...              regular expression (RE2)
//	1.2.3.0/24, 8.8.8.8, 2001:db8::/32       subnets and addresses
//
// The kinds match the rule types of MagiTrickle (Namespace, Domain,
// Wildcard, RegExp). Invalid regular expressions are dropped.
func ParseEntries(body string) Entries {
	var out Entries
	seen := map[string]bool{}
	add := func(dst *[]string, kind, v string) {
		if k := kind + "\x00" + v; !seen[k] {
			seen[k] = true
			*dst = append(*dst, v)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//") {
			continue
		}
		prefix, rest := splitPrefix(line)
		if prefix == "regexp" || prefix == "regex" {
			// A regular expression may contain "#"; only a trailing " #" is a comment.
			if i := strings.Index(rest, " #"); i >= 0 {
				rest = rest[:i]
			}
			if rest = strings.TrimSpace(rest); rest != "" {
				if _, err := regexp.Compile(rest); err == nil {
					add(&out.Regex, "r", rest)
				}
			}
			continue
		}
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
			prefix, rest = splitPrefix(line)
		}
		if line == "" {
			continue
		}
		if ip := net.ParseIP(line); ip != nil {
			if ip.To4() != nil {
				line += "/32"
			} else {
				line += "/128"
			}
		}
		if _, _, err := net.ParseCIDR(line); err == nil {
			add(&out.CIDR, "c", line)
			continue
		}
		kind := prefix
		if kind != "" {
			line = rest
		}
		line = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(line), "."))
		if line == "" {
			continue
		}
		switch kind {
		case "keyword":
			if !strings.ContainsAny(line, " /\t") {
				add(&out.Keyword, "k", line)
			}
		case "full", "exact":
			if validDomain(line) {
				add(&out.Exact, "e", line)
			}
		case "wildcard":
			if re := WildcardToRegexp(line); re != "" {
				add(&out.Regex, "r", re)
			}
		default: // "", namespace, domain, suffix
			if kind == "" && strings.ContainsAny(line, "*?") && !isLeadingStarOnly(line) {
				if re := WildcardToRegexp(line); re != "" {
					add(&out.Regex, "r", re)
				}
				continue
			}
			line = strings.TrimPrefix(strings.TrimPrefix(line, "*"), ".")
			if validDomain(line) {
				add(&out.Suffix, "s", line)
			}
		}
	}
	return out
}

// splitPrefix recognises the "kind:value" form. Unknown prefixes (and IPv6
// addresses) are not prefixes.
func splitPrefix(line string) (kind, rest string) {
	i := strings.IndexByte(line, ':')
	if i <= 0 {
		return "", line
	}
	switch k := strings.ToLower(line[:i]); k {
	case "namespace", "domain", "suffix", "full", "exact", "keyword", "wildcard", "regexp", "regex":
		return k, strings.TrimSpace(line[i+1:])
	}
	return "", line
}

func isLeadingStarOnly(s string) bool {
	return strings.HasPrefix(s, "*.") && !strings.ContainsAny(s[2:], "*?")
}

func validDomain(s string) bool {
	return s != "" && s != "localhost" && strings.Contains(s, ".") && !strings.ContainsAny(s, " /:\t*?")
}

// WildcardToRegexp turns a MagiTrickle-style wildcard ("*" is any characters,
// "?" exactly one) into an anchored regular expression. It returns "" for an
// empty pattern.
func WildcardToRegexp(w string) string {
	w = strings.TrimSpace(w)
	if w == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("^")
	for _, r := range w {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return b.String()
}
