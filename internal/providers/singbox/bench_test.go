package singbox

import (
	"fmt"
	"testing"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/subscription"
)

// A large subscription with many identical names and a big list must build
// quickly: the config is rebuilt on every subscription or list update.
func BenchmarkBuildLargeSubscription(b *testing.B) {
	cfg := config.DefaultConfig()
	cfg.Sections = []config.Section{{Name: "main", Enabled: true, CommunityLists: []string{"big"}}}
	cfg.CommunityLists = []config.CommunityList{{Name: "big", URL: "https://x.example/big.lst"}}

	var infos []subscription.OutboundInfo
	for i := 0; i < 5000; i++ {
		infos = append(infos, subscription.OutboundInfo{Type: "trojan", Name: "Node", Server: fmt.Sprintf("h%d.example", i), Port: 443, Password: "p", Security: "tls"})
	}
	domains := make([]string, 100000)
	for i := range domains {
		domains[i] = fmt.Sprintf("d%d.example.com", i)
	}
	nodes := fakeNodes{"main": infos}
	lists := fakeLists{"https://x.example/big.lst": {domains, nil}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := ConfigFromSettings(cfg, nodes, WithLists(lists))
		if _, err := c.Render(); err != nil {
			b.Fatal(err)
		}
	}
}
