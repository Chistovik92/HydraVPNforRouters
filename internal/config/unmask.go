package config

import (
	"errors"
	"fmt"
)

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

// ErrMaskedListChanged: a list sent back with masked items no longer has
// the stored length, so the items cannot be matched to the stored secrets.
var ErrMaskedListChanged = errors.New("a list with hidden items (********) changed its length: send the whole list again or keep it as it was")

// keepAll restores masked list items by position. Masked items can only be
// matched when the list kept its length (items may be edited in place).
func keepAll(incoming, stored []string) ([]string, error) {
	if incoming == nil {
		return nil, nil
	}
	out := make([]string, len(incoming))
	for i, v := range incoming {
		if v != masked {
			out[i] = v
			continue
		}
		if len(incoming) != len(stored) {
			return nil, ErrMaskedListChanged
		}
		out[i] = stored[i]
	}
	return out, nil
}

// UnmaskSection restores the masked secrets of s from stored.
func UnmaskSection(s, stored Section) (Section, error) {
	var err error
	if s.SelectorProxyLinks, err = keepAll(s.SelectorProxyLinks, stored.SelectorProxyLinks); err != nil {
		return s, fmt.Errorf("selector_proxy_links: %w", err)
	}
	if s.OutboundJsons, err = keepAll(s.OutboundJsons, stored.OutboundJsons); err != nil {
		return s, fmt.Errorf("outbound_jsons: %w", err)
	}
	return s, nil
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
