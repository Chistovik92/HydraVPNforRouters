package mgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/providers/singbox"
)

// ClashClient talks to the local sing-box Clash API.
type ClashClient struct {
	base   string
	client *http.Client
}

// NewClashClient creates a client for host:port.
func NewClashClient(addr string) *ClashClient {
	return &ClashClient{base: "http://" + addr, client: &http.Client{Timeout: 20 * time.Second}}
}

// Node is one outbound as seen by the management API.
type Node struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Now     string   `json:"now,omitempty"`     // selected member of a group
	Members []string `json:"members,omitempty"` // members of a group
	DelayMS int      `json:"delay_ms,omitempty"`
	Country string   `json:"country,omitempty"`
}

// Nodes lists groups and proxies with the last measured latency.
func (c *ClashClient) Nodes(ctx context.Context) ([]Node, error) {
	var resp struct {
		Proxies map[string]struct {
			Type    string   `json:"type"`
			Now     string   `json:"now"`
			All     []string `json:"all"`
			History []struct {
				Delay int `json:"delay"`
			} `json:"history"`
		} `json:"proxies"`
	}
	if err := c.do(ctx, http.MethodGet, "/proxies", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(resp.Proxies))
	for name, p := range resp.Proxies {
		n := Node{Name: name, Type: p.Type, Now: p.Now, Members: p.All, Country: singbox.CountryOf(name)}
		if len(p.History) > 0 {
			n.DelayMS = p.History[len(p.History)-1].Delay
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Select chooses the active member of a selector group.
func (c *ClashClient) Select(ctx context.Context, group, node string) error {
	body, _ := json.Marshal(map[string]string{"name": node})
	return c.do(ctx, http.MethodPut, "/proxies/"+url.PathEscape(group), body, nil)
}

// Delay measures the latency of a node in milliseconds.
func (c *ClashClient) Delay(ctx context.Context, node, testURL string) (int, error) {
	var resp struct {
		Delay int `json:"delay"`
	}
	q := url.Values{"url": {testURL}, "timeout": {"8000"}}
	if err := c.do(ctx, http.MethodGet, "/proxies/"+url.PathEscape(node)+"/delay?"+q.Encode(), nil, &resp); err != nil {
		return 0, err
	}
	return resp.Delay, nil
}

func (c *ClashClient) do(ctx context.Context, method, path string, body []byte, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("sing-box API is not reachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("sing-box API: HTTP %d %s", resp.StatusCode, e.Message)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
