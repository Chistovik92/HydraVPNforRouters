#!/usr/bin/env ucode

function as_string(value) {
    return value == null ? "" : "" + value;
}

function env(name, fallback) {
    let value = getenv(name);
    return value == null ? as_string(fallback) : as_string(value);
}

function shell_quote(value) {
    return "'" + replace(as_string(value), /'/g, "'\\''") + "'";
}

function constants_map() {
    let c = {};

    c.PODKOP_VERSION = env("PODKOP_VERSION", "1.0.0");
    c.PODKOP_CONFIG_NAME = env("PODKOP_CONFIG_NAME", "podkop");
    c.PODKOP_CONFIG = env("PODKOP_CONFIG", "/etc/config/" + c.PODKOP_CONFIG_NAME);
    c.PODKOP_BIN = env("PODKOP_BIN", "/usr/bin/podkop");
    c.PODKOP_SERVICE_NAME = env("PODKOP_SERVICE_NAME", "podkop");
    c.PODKOP_SERVICE_INIT = env("PODKOP_SERVICE_INIT", "/etc/init.d/podkop");
    c.PODKOP_LIB_DIR = env("PODKOP_LIB_DIR", "/usr/lib/podkop");

    c.RESOLV_CONF = env("RESOLV_CONF", "/etc/resolv.conf");
    c.CHECK_PROXY_IP_DOMAIN = env("CHECK_PROXY_IP_DOMAIN", "ip.podkop.fyi");
    c.FAKEIP_TEST_DOMAIN = env("FAKEIP_TEST_DOMAIN", "fakeip.podkop.fyi");
    c.TMP_SING_BOX_FOLDER = env("TMP_SING_BOX_FOLDER", "/tmp/sing-box");
    c.TMP_RULESET_FOLDER = env("TMP_RULESET_FOLDER", c.TMP_SING_BOX_FOLDER + "/rulesets");
    c.TMP_SUBSCRIPTION_FOLDER = env("TMP_SUBSCRIPTION_FOLDER", c.TMP_SING_BOX_FOLDER + "/subscriptions");
    c.CLOUDFLARE_OCTETS = env("CLOUDFLARE_OCTETS", "8.47 162.159 188.114");
    c.RT_TABLE_NAME = env("RT_TABLE_NAME", "podkop");

    c.NFT_TABLE_NAME = env("NFT_TABLE_NAME", "PodkopTable");
    c.NFT_LOCALV4_SET_NAME = env("NFT_LOCALV4_SET_NAME", "localv4");
    c.NFT_LOCALV6_SET_NAME = env("NFT_LOCALV6_SET_NAME", "localv6");
    c.NFT_COMMON_SET_NAME = env("NFT_COMMON_SET_NAME", "podkop_subnets");
    c.NFT_COMMON6_SET_NAME = env("NFT_COMMON6_SET_NAME", "podkop_subnets6");
    c.NFT_PORT_SET_NAME = env("NFT_PORT_SET_NAME", "podkop_ports");
    c.NFT_IP_PORT_SET_NAME = env("NFT_IP_PORT_SET_NAME", "podkop_ip_ports");
    c.NFT_IP_PORT6_SET_NAME = env("NFT_IP_PORT6_SET_NAME", "podkop_ip6_ports");
    c.NFT_INTERFACE_SET_NAME = env("NFT_INTERFACE_SET_NAME", "podkop_interfaces");
    c.NFT_FAKEIP_MARK = env("NFT_FAKEIP_MARK", "0x04000000");
    c.NFT_OUTBOUND_MARK = env("NFT_OUTBOUND_MARK", "0x08000000");

    c.SB_REQUIRED_VERSION = env("SB_REQUIRED_VERSION", "1.12.0");
    c.SB_MANAGED_SERVICE_MARKER = env("SB_MANAGED_SERVICE_MARKER", "Podkop managed sing-box service");
    c.SB_DNS_SERVER_TAG = env("SB_DNS_SERVER_TAG", "dns-server");
    c.SB_FAKEIP_DNS_SERVER_TAG = env("SB_FAKEIP_DNS_SERVER_TAG", "fakeip-server");
    c.SB_FAKEIP_INET4_RANGE = env("SB_FAKEIP_INET4_RANGE", "198.18.0.0/15");
    c.SB_FAKEIP_INET6_RANGE = env("SB_FAKEIP_INET6_RANGE", "fc00::/18");
    c.SB_BOOTSTRAP_SERVER_TAG = env("SB_BOOTSTRAP_SERVER_TAG", "bootstrap-dns-server");
    c.SB_FAKEIP_DNS_RULE_TAG = env("SB_FAKEIP_DNS_RULE_TAG", "fakeip-dns-rule-tag");
    c.SB_FAKEIP_RULESET_DNS_RULE_TAG = env("SB_FAKEIP_RULESET_DNS_RULE_TAG", "fakeip-ruleset-dns-rule-tag");
    c.SB_SERVICE_FAKEIP_DNS_RULE_TAG = env("SB_SERVICE_FAKEIP_DNS_RULE_TAG", "service-fakeip-dns-rule-tag");
    c.SB_TPROXY_INBOUND_TAG = env("SB_TPROXY_INBOUND_TAG", "tproxy-in");
    c.SB_TPROXY_INBOUND_ADDRESS = env("SB_TPROXY_INBOUND_ADDRESS", "0.0.0.0");
    c.SB_TPROXY_INBOUND6_TAG = env("SB_TPROXY_INBOUND6_TAG", "tproxy6-in");
    c.SB_TPROXY_INBOUND6_ADDRESS = env("SB_TPROXY_INBOUND6_ADDRESS", "::");
    c.SB_TPROXY_INBOUND_PORT = env("SB_TPROXY_INBOUND_PORT", "1602");
    c.SB_DNS_INBOUND_TAG = env("SB_DNS_INBOUND_TAG", "dns-in");
    c.SB_DNS_INBOUND_ADDRESS = env("SB_DNS_INBOUND_ADDRESS", "127.0.0.42");
    c.SB_DNS_INBOUND_PORT = env("SB_DNS_INBOUND_PORT", "53");
    c.SB_DIRECT_OUTBOUND_TAG = env("SB_DIRECT_OUTBOUND_TAG", "direct-out");
    c.SB_BYPASS_OUTBOUND_TAG = env("SB_BYPASS_OUTBOUND_TAG", "bypass-out");
    c.SB_CLASH_API_CONTROLLER_PORT = env("SB_CLASH_API_CONTROLLER_PORT", "9090");
    c.SB_VARIANT_STATE_FILE = env("SB_VARIANT_STATE_FILE", "/etc/podkop/sing-box-variant");
    c.SB_VERSION_STATE_FILE = env("SB_VERSION_STATE_FILE", "/etc/podkop/sing-box-version");

    c.GITHUB_RAW_URL = env("GITHUB_RAW_URL", "https://raw.githubusercontent.com/itdoginfo/allow-domains/main");
    c.SRS_MAIN_URL = env("SRS_MAIN_URL", "https://github.com/itdoginfo/allow-domains/releases/latest/download");
    c.SRS_ADS_HAGEZI_PRO_URL = env("SRS_ADS_HAGEZI_PRO_URL", "https://github.com/zxc-rv/ad-filter/releases/latest/download/adlist.srs");
    c.SUBNETS_TWITTER = env("SUBNETS_TWITTER", c.GITHUB_RAW_URL + "/Subnets/IPv4/twitter.lst");
    c.SUBNETS_META = env("SUBNETS_META", c.GITHUB_RAW_URL + "/Subnets/IPv4/meta.lst");
    c.SUBNETS_DISCORD = env("SUBNETS_DISCORD", c.GITHUB_RAW_URL + "/Subnets/IPv4/discord.lst");
    c.SUBNETS_ROBLOX = env("SUBNETS_ROBLOX", c.GITHUB_RAW_URL + "/Subnets/IPv4/roblox.lst");
    c.SUBNETS_TELEGRAM = env("SUBNETS_TELEGRAM", c.GITHUB_RAW_URL + "/Subnets/IPv4/telegram.lst");
    c.SUBNETS_CLOUDFLARE = env("SUBNETS_CLOUDFLARE", c.GITHUB_RAW_URL + "/Subnets/IPv4/cloudflare.lst");
    c.SUBNETS_HETZNER = env("SUBNETS_HETZNER", c.GITHUB_RAW_URL + "/Subnets/IPv4/hetzner.lst");
    c.SUBNETS_OVH = env("SUBNETS_OVH", c.GITHUB_RAW_URL + "/Subnets/IPv4/ovh.lst");
    c.SUBNETS_DIGITALOCEAN = env("SUBNETS_DIGITALOCEAN", c.GITHUB_RAW_URL + "/Subnets/IPv4/digitalocean.lst");
    c.SUBNETS_CLOUDFRONT = env("SUBNETS_CLOUDFRONT", c.GITHUB_RAW_URL + "/Subnets/IPv4/cloudfront.lst");
    c.COMMUNITY_SERVICES = env("COMMUNITY_SERVICES", "russia_inside russia_outside ukraine_inside geoblock block porn news anime youtube hdrezka tiktok google_ai google_play hodca discord meta twitter cloudflare cloudfront digitalocean hetzner ovh telegram roblox ads_hagezi_pro");

    return c;
}

function print_shell_env(constants) {
    for (let name in sort(keys(constants)))
        print(name, "=", shell_quote(constants[name]), "\n");
}

function module_exports() {
    return constants_map();
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

let mode = ARGV[0] || "";
let constants = constants_map();

if (mode == "shell-env")
    print_shell_env(constants);
else if (mode == "json")
    print(sprintf("%J", constants), "\n");
else if (mode == "get")
    print(as_string(constants[ARGV[1]]), "\n");
else {
    warn("Usage: constants.uc <shell-env|json|get> ...\n");
    exit(1);
}