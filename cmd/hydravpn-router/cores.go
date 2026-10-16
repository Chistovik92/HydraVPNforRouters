package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/core"
	"github.com/Chistovik92/hydravpn-router/internal/cores"
)

// CoreCmd installs and lists core versions from the command line (the same
// installer as POST /api/v1/cores/{name}/install; install.sh uses it).
type CoreCmd struct {
	List     CoreListCmd     `cmd:"" help:"Show the installed cores and the versions kept for a rollback"`
	Versions CoreVersionsCmd `cmd:"" help:"Show the versions of a core that have a build for this router (betas included)"`
	Install  CoreInstallCmd  `cmd:"" help:"Install a core version: x.y.z, latest (newest stable) or beta (newest)"`
}

type coreArgs struct {
	ConfigFile string `short:"c" help:"Configuration file path" default:"${config_file}"`
}

func (a coreArgs) installer() *cores.Installer {
	return cores.NewInstaller(cores.InstallerOptions{Dir: core.CoresDir(a.ConfigFile)})
}

func coreKind(name string) (cores.Kind, error) {
	names := make([]string, len(cores.Installable))
	for i, k := range cores.Installable {
		if string(k) == name {
			return k, nil
		}
		names[i] = string(k)
	}
	return "", fmt.Errorf("unknown core %q (one of: %s)", name, strings.Join(names, ", "))
}

type CoreListCmd struct{ coreArgs }

func (c *CoreListCmd) Run() error {
	inst := c.installer()
	for _, k := range cores.Installable {
		state := "not installed by the service"
		if active := cores.Resolve(inst.Link(k)); active != inst.Link(k) {
			state = "active: " + active
		}
		fmt.Printf("%-10s %s; kept: %s\n", k, state, strings.Join(inst.Versions(k), ", "))
	}
	return nil
}

type CoreVersionsCmd struct {
	coreArgs
	Name string `arg:"" help:"sing-box, xray, openflux, olcrtc, amneziawg"`
}

func (c *CoreVersionsCmd) Run() error {
	k, err := coreKind(c.Name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	rels, err := c.installer().Releases(ctx, k)
	if err != nil {
		return err
	}
	for _, r := range rels {
		mark := ""
		if r.Prerelease {
			mark = " (beta)"
		}
		fmt.Println(r.Version + mark)
	}
	return nil
}

type CoreInstallCmd struct {
	coreArgs
	Name    string `arg:"" help:"sing-box, xray, openflux, olcrtc, amneziawg"`
	Version string `arg:"" optional:"" default:"latest" help:"x.y.z, latest or beta"`
}

func (c *CoreInstallCmd) Run() error {
	k, err := coreKind(c.Name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	inst := c.installer()
	v, err := inst.InstallNow(ctx, k, c.Version)
	if err != nil {
		return err
	}
	fmt.Printf("%s %s installed: %s\n", k, v, inst.Link(k))
	if k == cores.SingBox {
		// Point the service at the managed link (the node cores are found
		// there without a setting).
		edit := config.Edit{Key: "settings", Op: config.OpSet, Name: "singbox_binary", Value: inst.Link(k)}
		if _, err := config.ApplyEdits(c.ConfigFile, []config.Edit{edit}); err != nil {
			return fmt.Errorf("set singbox_binary: %w", err)
		}
		fmt.Println("singbox_binary now points at it")
	}
	fmt.Println("Apply to the running service: hydravpn-router reload")
	return nil
}
