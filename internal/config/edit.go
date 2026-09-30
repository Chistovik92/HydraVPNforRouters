package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Edit operations.
const (
	OpAppend        = "append"        // add Value to the list Key
	OpRemoveIndex   = "remove_index"  // remove the item at Index
	OpRemoveByName  = "remove_name"   // remove the item whose "name" is Name
	OpReplaceByName = "replace_name"  // replace the item whose "name" is Name with Value
	OpReplaceIndex  = "replace_index" // replace the item at Index with Value
)

// Edit changes one top-level list of the configuration file
// (sections, subscription_urls, servers, ...).
type Edit struct {
	Key   string
	Op    string
	Index int
	Name  string
	Value interface{}
}

// ApplyEdits applies edits to the file at path and saves the result.
//
// The file is edited as a YAML tree, so comments, the order of keys and the
// rest of the hand-written content stay as they are (blank lines between
// blocks are not kept by the YAML library). The result is validated before
// anything is written; the previous file is kept as path+".bak". A missing
// file is created. The new configuration is returned.
func ApplyEdits(path string, edits []Edit) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	var doc yaml.Node
	if len(bytes.TrimSpace(data)) > 0 {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	root := documentRoot(&doc)

	for _, e := range edits {
		if err := applyEdit(root, e); err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	enc.Close()

	cfg, err := parseConfig(buf.Bytes(), path)
	if err != nil {
		return nil, err // nothing was written
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	if len(data) > 0 {
		_ = os.WriteFile(path+".bak", data, 0600)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	return cfg, nil
}

// documentRoot returns the top-level mapping of doc, creating it if the
// document is empty.
func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind != yaml.DocumentNode {
		doc.Kind = yaml.DocumentNode
		doc.Content = nil
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	return doc.Content[0]
}

// listNode returns the sequence stored under key, creating it (or turning a
// null / flow "[]" value into a block sequence) as needed.
func listNode(root *yaml.Node, key string) (*yaml.Node, error) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		v := root.Content[i+1]
		switch v.Kind {
		case yaml.SequenceNode:
			return v, nil
		case yaml.ScalarNode:
			if v.Tag == "!!null" || v.Value == "" {
				seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq",
					HeadComment: v.HeadComment, LineComment: v.LineComment, FootComment: v.FootComment}
				root.Content[i+1] = seq
				return seq, nil
			}
		}
		return nil, fmt.Errorf("%q is not a list in the config file", key)
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, seq)
	return seq, nil
}

func applyEdit(root *yaml.Node, e Edit) error {
	seq, err := listNode(root, e.Key)
	if err != nil {
		return err
	}
	find := func() int {
		for i, item := range seq.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(item.Content); j += 2 {
				if item.Content[j].Value == "name" && item.Content[j+1].Value == e.Name {
					return i
				}
			}
		}
		return -1
	}

	switch e.Op {
	case OpAppend:
		n, err := valueNode(e.Value)
		if err != nil {
			return err
		}
		seq.Style = 0 // a "[]" flow list becomes a block list
		seq.Content = append(seq.Content, n)
	case OpRemoveIndex:
		if e.Index < 0 || e.Index >= len(seq.Content) {
			return fmt.Errorf("%s: no item %d", e.Key, e.Index)
		}
		seq.Content = append(seq.Content[:e.Index], seq.Content[e.Index+1:]...)
	case OpRemoveByName:
		i := find()
		if i < 0 {
			return fmt.Errorf("%s: no item named %q", e.Key, e.Name)
		}
		seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
	case OpReplaceIndex:
		if e.Index < 0 || e.Index >= len(seq.Content) {
			return fmt.Errorf("%s: no item %d", e.Key, e.Index)
		}
		n, err := valueNode(e.Value)
		if err != nil {
			return err
		}
		old := seq.Content[e.Index]
		n.HeadComment, n.LineComment, n.FootComment = old.HeadComment, old.LineComment, old.FootComment
		seq.Content[e.Index] = n
	case OpReplaceByName:
		i := find()
		if i < 0 {
			return fmt.Errorf("%s: no item named %q", e.Key, e.Name)
		}
		n, err := valueNode(e.Value)
		if err != nil {
			return err
		}
		n.HeadComment, n.LineComment, n.FootComment = seq.Content[i].HeadComment, seq.Content[i].LineComment, seq.Content[i].FootComment
		seq.Content[i] = n
	default:
		return fmt.Errorf("unknown edit operation %q", e.Op)
	}
	if len(seq.Content) == 0 {
		seq.Style = yaml.FlowStyle // prints as "[]"
	}
	return nil
}

// valueNode encodes v as a YAML node without the fields that only hold their
// zero value, so an added entry stays as short as one written by hand.
func valueNode(v interface{}) (*yaml.Node, error) {
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return nil, err
	}
	prune(&n)
	return &n, nil
}

func prune(n *yaml.Node) {
	if n.Kind != yaml.MappingNode {
		return
	}
	kept := n.Content[:0]
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		prune(v)
		if isZero(v) {
			continue
		}
		kept = append(kept, k, v)
	}
	n.Content = kept
}

func isZero(v *yaml.Node) bool {
	switch v.Kind {
	case yaml.ScalarNode:
		switch v.Tag {
		case "!!null":
			return true
		case "!!str":
			return v.Value == "" || v.Value == "0s" // "0s" is a zero time.Duration
		case "!!bool":
			return v.Value == "false"
		case "!!int", "!!float":
			return v.Value == "0" || v.Value == "0.0"
		}
	case yaml.SequenceNode, yaml.MappingNode:
		return len(v.Content) == 0
	}
	return false
}
