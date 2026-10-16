package mgmt

import (
	"reflect"
	"strings"

	"github.com/Chistovik92/hydravpn-router/internal/config"
)

// diffConfig turns the difference between two configurations into edits of
// the file. Only the lists the API changes are compared; a change is one
// appended, removed or replaced item (anything else rewrites the list).
func diffConfig(before, after *config.Config) []config.Edit {
	var edits []config.Edit
	edits = append(edits, diffList("sections", before.Sections, after.Sections, func(s config.Section) string { return s.Name })...)
	edits = append(edits, diffList("subscription_urls", before.SubscriptionURLs, after.SubscriptionURLs, nil)...)
	edits = append(edits, diffList("servers", before.Servers, after.Servers, func(s config.Server) string { return s.Name })...)
	edits = append(edits, diffList("rules", before.Rules, after.Rules, nil)...)
	edits = append(edits, diffList("community_lists", before.CommunityLists, after.CommunityLists, nil)...)
	edits = append(edits, diffSettings(before.Settings, after.Settings)...)
	return edits
}

// diffSettings sets each changed key of settings on its own, so the rest of
// the hand-written block stays as it is.
func diffSettings(before, after config.Settings) []config.Edit {
	var edits []config.Edit
	b, a := reflect.ValueOf(before), reflect.ValueOf(after)
	for i := 0; i < b.NumField(); i++ {
		if reflect.DeepEqual(b.Field(i).Interface(), a.Field(i).Interface()) {
			continue
		}
		key, _, _ := strings.Cut(b.Type().Field(i).Tag.Get("yaml"), ",")
		edits = append(edits, config.Edit{Key: "settings", Op: config.OpSet, Name: key, Value: a.Field(i).Interface()})
	}
	return edits
}

func diffList[T any](key string, before, after []T, name func(T) string) []config.Edit {
	if reflect.DeepEqual(before, after) {
		return nil
	}
	// Appended items.
	if len(after) > len(before) && reflect.DeepEqual(before, after[:len(before)]) {
		var e []config.Edit
		for _, v := range after[len(before):] {
			e = append(e, config.Edit{Key: key, Op: config.OpAppend, Value: v})
		}
		return e
	}
	// One removed item.
	if len(after) == len(before)-1 {
		for i := range before {
			rest := append(append([]T{}, before[:i]...), before[i+1:]...)
			if reflect.DeepEqual(rest, after) {
				return []config.Edit{{Key: key, Op: config.OpRemoveIndex, Index: i}}
			}
		}
	}
	// One replaced item.
	if len(after) == len(before) {
		diff := -1
		for i := range before {
			if !reflect.DeepEqual(before[i], after[i]) {
				if diff >= 0 {
					diff = -2
					break
				}
				diff = i
			}
		}
		if diff >= 0 {
			return []config.Edit{{Key: key, Op: config.OpReplaceIndex, Index: diff, Value: after[diff]}}
		}
	}
	// Anything else: rewrite the whole list.
	var e []config.Edit
	for i := len(before) - 1; i >= 0; i-- {
		e = append(e, config.Edit{Key: key, Op: config.OpRemoveIndex, Index: i})
	}
	for _, v := range after {
		e = append(e, config.Edit{Key: key, Op: config.OpAppend, Value: v})
	}
	return e
}
