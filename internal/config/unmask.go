package config

// A client edits what the API returned, and that copy carries "********"
// instead of secrets. The helpers below put the stored values back so that
// writing an edited item does not overwrite keys with the mask.

// IsMasked reports whether s is the placeholder the API sends for a secret.
func IsMasked(s string) bool { return s == masked }

func keep(incoming, stored string) string {
	if incoming == masked {
		return stored
	}
	return incoming
}

// keepAll restores masked list items by position when the list kept its
// length; otherwise masked items are dropped (they cannot be matched to a
// stored value).
func keepAll(incoming, stored []string) []string {
	if incoming == nil {
		return nil
	}
	out := make([]string, 0, len(incoming))
	for i, v := range incoming {
		switch {
		case v != masked:
			out = append(out, v)
		case len(incoming) == len(stored):
			out = append(out, stored[i])
		}
	}
	return out
}

// UnmaskSection restores the masked secrets of s from stored.
func UnmaskSection(s, stored Section) Section {
	s.SelectorProxyLinks = keepAll(s.SelectorProxyLinks, stored.SelectorProxyLinks)
	s.OutboundJsons = keepAll(s.OutboundJsons, stored.OutboundJsons)
	return s
}

// UnmaskServer restores the masked secrets of s from stored.
func UnmaskServer(s, stored Server) Server {
	s.ServerUUID = keep(s.ServerUUID, stored.ServerUUID)
	s.RealityPrivateKey = keep(s.RealityPrivateKey, stored.RealityPrivateKey)
	s.MTProtoSecret = keep(s.MTProtoSecret, stored.MTProtoSecret)
	s.TailscaleAuthKey = keep(s.TailscaleAuthKey, stored.TailscaleAuthKey)
	s.InboundJSON = keep(s.InboundJSON, stored.InboundJSON)
	return s
}

// UnmaskSubscription keeps the stored URL when the client sent the masked
// form of it back.
func UnmaskSubscription(s, stored SubscriptionURL) SubscriptionURL {
	if s.URL == MaskURL(stored.URL) {
		s.URL = stored.URL
	}
	return s
}
