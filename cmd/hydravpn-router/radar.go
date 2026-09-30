package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/mgmt"
)

// RadarCmd links the router to an account in the Radar bot and pulls the
// subscriptions issued there. The commands talk to the running service through
// its management API (api_listen must be set): the service owns the config.
type RadarCmd struct {
	Link   RadarLinkCmd   `cmd:"" help:"Link to a bot account with the one-time code (bot: VPN -> Connect an app)"`
	Sync   RadarSyncCmd   `cmd:"" help:"Fetch the subscriptions issued in the bot and add the new ones"`
	Status RadarStatusCmd `cmd:"" help:"Show whether the router is linked to a bot account"`
	Unlink RadarUnlinkCmd `cmd:"" help:"Disconnect this router from the bot account"`
}

type radarCommon struct {
	ConfigFile string `short:"c" help:"Configuration file path" default:"${config_file}"`
}

type RadarLinkCmd struct {
	radarCommon
	Server  string `help:"Address of the bot server, for example radar.example.org" required:""`
	Code    string `help:"8-digit one-time code from the bot" required:""`
	Section string `help:"Section to put the subscriptions into (default: main, else the first)"`
}

func (c *RadarLinkCmd) Run(g *Globals) error {
	return radarCall(g, c.ConfigFile, http.MethodPost, "/api/v1/radar/link",
		map[string]string{"server": c.Server, "code": c.Code, "section": c.Section})
}

type RadarSyncCmd struct {
	radarCommon
	Section string `help:"Section to put the subscriptions into (default: main, else the first)"`
}

func (c *RadarSyncCmd) Run(g *Globals) error {
	return radarCall(g, c.ConfigFile, http.MethodPost, "/api/v1/radar/sync",
		map[string]string{"section": c.Section})
}

type RadarStatusCmd struct{ radarCommon }

func (c *RadarStatusCmd) Run(g *Globals) error {
	return radarCall(g, c.ConfigFile, http.MethodGet, "/api/v1/radar", nil)
}

type RadarUnlinkCmd struct{ radarCommon }

func (c *RadarUnlinkCmd) Run(g *Globals) error {
	return radarCall(g, c.ConfigFile, http.MethodDelete, "/api/v1/radar", nil)
}

// radarCall sends one request to the local management API and prints the
// answer. A status of 400 or more makes the command fail.
func radarCall(g *Globals, configFile, method, path string, body interface{}) error {
	cfg, err := config.LoadFromFile(configFile)
	if err != nil {
		return err
	}
	s := cfg.Settings
	if s.APIListen == "" {
		return errors.New("api_listen is not set: the service needs its management API for this command")
	}
	token := s.APIToken
	if token == "" {
		data, err := os.ReadFile(mgmt.TokenFile(g.RuntimeDir))
		if err != nil {
			return errors.New("no API token yet: start the service first")
		}
		token = strings.TrimSpace(string(data))
	}

	host, port, err := net.SplitHostPort(s.APIListen)
	if err != nil {
		return fmt.Errorf("api_listen %q: %w", s.APIListen, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	scheme := "http"
	client := &http.Client{Timeout: 90 * time.Second}
	if s.APITLSCert != "" && s.APITLSKey != "" {
		scheme = "https"
		// The certificate is usually self-signed: pin its fingerprint instead of trusting a CA.
		fp, err := mgmt.CertFingerprint(s.APITLSCert)
		if err != nil {
			return err
		}
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // replaced by the fingerprint check below
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
				if len(raw) == 0 {
					return errors.New("no certificate")
				}
				sum := sha256.Sum256(raw[0])
				if hex.EncodeToString(sum[:]) != fp {
					return errors.New("the API certificate does not match the configured one")
				}
				return nil
			},
		}}
	}

	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, scheme+"://"+net.JoinHostPort(host, port)+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("the service does not answer (is it running?): %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var pretty bytes.Buffer
	if json.Indent(&pretty, data, "", "  ") == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(strings.TrimSpace(string(data)))
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
