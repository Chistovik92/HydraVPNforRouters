# HydraVPN for Router - installer for MikroTik RouterOS 7 (container)
#
# RouterOS has no POSIX shell, so scripts/install.sh does not run here. This
# script does the same job with RouterOS commands: it downloads the container
# image built for the router architecture, creates the container network and
# starts the container.
#
#   /tool fetch url="https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.rsc" dst-path=hydravpn-install.rsc
#   /import hydravpn-install.rsc
#
# Optional settings, set them before /import:
#   :global hydraVersion "1.2.5"            install this version (default: the one of this script)
#   :global hydraDisk "disk1"               where the image and the config live (default: disk1, or flash if there is no disk)
#   :global hydraRouteLan "192.168.88.0/24" also send this LAN through the container (routing table + mangle rule)
#
# Requirements: RouterOS 7.6+, the "container" package, container mode enabled
# (/system/device-mode/update container=yes + confirmation), ARM, ARM64 or x86.
# Not verified on real hardware: see docs/MIKROTIK.md.

:global hydraVersion
:global hydraDisk
:global hydraRouteLan

:local ver "1.2.5"
:if ([:typeof $hydraVersion] = "str" && [:len $hydraVersion] > 0) do={ :set ver $hydraVersion }
:local repo "Chistovik92/HydraVPNforRouters"

# ---- checks
:if ([:pick [/system resource get version] 0 1] != "7") do={
    :error "HydraVPN: RouterOS 7 is required"
}
:if ([:len [/system package find where name="container" and disabled=no]] = 0) do={
    :error "HydraVPN: the container package is not installed or is disabled (System > Packages)"
}

:local arch [/system resource get architecture-name]
:local img ""
:if ($arch = "arm64") do={ :set img "arm64" }
:if ($arch = "arm") do={ :set img "armv7" }
:if ($arch = "x86_64" || $arch = "x86") do={ :set img "amd64" }
:if ($img = "") do={
    :error ("HydraVPN: containers do not run on this architecture: " . $arch . " (ARM, ARM64 and x86 only)")
}

# ---- storage
:local disk "flash"
:if ([:len [/disk find]] > 0) do={ :set disk "disk1" }
:if ([:typeof $hydraDisk] = "str" && [:len $hydraDisk] > 0) do={ :set disk $hydraDisk }

# ---- download the image: hydravpn-router-routeros-<ver>-<arch>.tar
:local file ($disk . "/hydravpn-router-routeros-" . $ver . "-" . $img . ".tar")
:local url ("https://github.com/" . $repo . "/releases/download/v" . $ver . "/hydravpn-router-routeros-" . $ver . "-" . $img . ".tar")
:put ("HydraVPN: downloading " . $url)
/tool fetch url=$url dst-path=$file
:if ([:len [/file find where name=$file]] = 0) do={ :error "HydraVPN: download failed" }

# ---- container network: veth + bridge + NAT
:if ([:len [/interface veth find where name="veth-hydravpn"]] = 0) do={
    /interface veth add name=veth-hydravpn address=172.17.0.2/24 gateway=172.17.0.1
}
:if ([:len [/interface bridge find where name="br-containers"]] = 0) do={
    /interface bridge add name=br-containers
    /ip address add address=172.17.0.1/24 interface=br-containers
}
:if ([:len [/interface bridge port find where interface="veth-hydravpn"]] = 0) do={
    /interface bridge port add bridge=br-containers interface=veth-hydravpn
}
:if ([:len [/ip firewall nat find where comment="hydravpn container"]] = 0) do={
    /ip firewall nat add chain=srcnat src-address=172.17.0.0/24 action=masquerade comment="hydravpn container"
}

# ---- config mount and container
:if ([:len [/container mounts find where name="hydravpn-config"]] = 0) do={
    /container mounts add name=hydravpn-config src=($disk . "/hydravpn-config") dst=/etc/hydravpn-router
}
:if ([:len [/container find where comment="hydravpn"]] > 0) do={
    :put "HydraVPN: a container already exists. To update, stop and remove it first (the config stays in the mount):"
    :put "  /container stop [find comment=hydravpn]; /container remove [find comment=hydravpn]"
    :error "HydraVPN: already installed"
}
/container add file=$file interface=veth-hydravpn mounts=hydravpn-config root-dir=($disk . "/hydravpn") logging=yes start-on-boot=yes comment="hydravpn"
:put "HydraVPN: unpacking the image..."
:delay 20s
/container start [find where comment="hydravpn"]

# ---- optional: send a LAN through the container
:if ([:typeof $hydraRouteLan] = "str" && [:len $hydraRouteLan] > 0) do={
    :if ([:len [/routing table find where name="via-hydra"]] = 0) do={
        /routing table add name=via-hydra fib
        /ip route add dst-address=0.0.0.0/0 gateway=172.17.0.2 routing-table=via-hydra
    }
    /ip firewall address-list add list=hydra-clients address=$hydraRouteLan
    /ip firewall mangle add chain=prerouting src-address-list=hydra-clients dst-address-type=!local \
        action=mark-routing new-routing-mark=via-hydra passthrough=no comment="hydravpn: clients via container"
    :put ("HydraVPN: " . $hydraRouteLan . " is routed through the container")
}

:put ""
:put ("HydraVPN " . $ver . " installed (container, " . $img . ").")
:put "Next: edit the config in the mounted folder:"
:put ("  " . $disk . "/hydravpn-config/config.yaml  (settings.source_network_interfaces: [\"eth0\"], api_listen: \"172.17.0.2:8088\")")
:put "Then check:  /container shell [find comment=hydravpn]   and run:  hydravpn-router selftest"
:put "Routing of LAN traffic into the container: see docs/MIKROTIK.md"
