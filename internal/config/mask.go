package config

import "strings"

const masked = "********"

// Masked returns a copy of the configuration that is safe to print or send
// to a client: keys, tokens, passwords and the secret part of subscription
// URLs are replaced.
func (c *Config) Masked() *Config {
	out := *c
	out.Settings.APIToken = maskIf(c.Settings.APIToken)

	out.SubscriptionURLs = make([]SubscriptionURL, len(c.SubscriptionURLs))
	for i, s := range c.SubscriptionURLs {
		s.URL = MaskURL(s.URL)
		out.SubscriptionURLs[i] = s
	}
	out.Servers = make([]Server, len(c.Servers))
	for i, s := range c.Servers {
		s.ServerUUID = maskIf(s.ServerUUID)
		s.RealityPrivateKey = maskIf(s.RealityPrivateKey)
		s.MTProtoSecret = maskIf(s.MTProtoSecret)
		s.TailscaleAuthKey = maskIf(s.TailscaleAuthKey)
		s.InboundJSON = maskIf(s.InboundJSON)
		out.Servers[i] = s
	}
	out.Sections = make([]Section, len(c.Sections))
	for i, s := range c.Sections {
		// Proxy links and outbound JSON carry credentials.
		s.SelectorProxyLinks = maskAll(s.SelectorProxyLinks)
		s.OutboundJsons = maskAll(s.OutboundJsons)
		out.Sections[i] = s
	}
	return &out
}

func maskIf(s string) string {
	if s == "" {
		return ""
	}
	return masked
}

func maskAll(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i := range in {
		out[i] = masked
	}
	return out
}

// MaskURL keeps scheme and host and hides path, query and user info: the
// token that identifies a subscription lives there.
func MaskURL(s string) string {
	i := strings.Index(s, "://")
	if i < 0 {
		return s
	}
	rest := s[i+3:]
	if at := strings.LastIndex(strings.SplitN(rest, "/", 2)[0], "@"); at >= 0 {
		rest = rest[at+1:]
	}
	if j := strings.IndexAny(rest, "/?#"); j >= 0 {
		return s[:i+3] + rest[:j] + "/" + masked
	}
	return s[:i+3] + rest
}
