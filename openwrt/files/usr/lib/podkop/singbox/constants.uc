#!/usr/bin/env ucode

function as_string(value) {
    return value == null ? "" : "" + value;
}

function env(name, fallback) {
    let value = getenv(name);
    return value == null ? as_string(fallback) : as_string(value);
}

function constants_map() {
    let c = {};

    c.TPROXY_INBOUND_TAG = env("TPROXY_INBOUND_TAG", "tproxy-in");
    c.TPROXY_INBOUND_ADDRESS = env("TPROXY_INBOUND_ADDRESS", "0.0.0.0");
    c.TPROXY_INBOUND6_TAG = env("TPROXY_INBOUND6_TAG", "tproxy6-in");
    c.TPROXY_INBOUND6_ADDRESS = env("TPROXY_INBOUND6_ADDRESS", "::");
    c.TPROXY_INBOUND_PORT = env("TPROXY_INBOUND_PORT", "1602");

    c.DNS_INBOUND_TAG = env("DNS_INBOUND_TAG", "dns-in");
    c.DNS_INBOUND_ADDRESS = env("DNS_INBOUND_ADDRESS", "127.0.0.42");
    c.DNS_INBOUND_PORT = env("DNS_INBOUND_PORT", "53");

    c.SOURCE_DNS_INBOUND_TAG = env("SOURCE_DNS_INBOUND_TAG", "source-dns-in");
    c.SOURCE_DNS_INBOUND_ADDRESS = env("SOURCE_DNS_INBOUND_ADDRESS", "127.0.0.43");
    c.SOURCE_DNS_INBOUND_PORT = env("SOURCE_DNS_INBOUND_PORT", "53");

    c.DNS_SERVER_TAG = env("DNS_SERVER_TAG", "dns-server");
    c.FAKEIP_DNS_SERVER_TAG = env("FAKEIP_DNS_SERVER_TAG", "fakeip-server");
    c.FAKEIP_INET4_RANGE = env("FAKEIP_INET4_RANGE", "198.18.0.0/15");
    c.FAKEIP_INET6_RANGE = env("FAKEIP_INET6_RANGE", "fc00::/18");
    c.BOOTSTRAP_SERVER_TAG = env("BOOTSTRAP_SERVER_TAG", "bootstrap-dns-server");

    c.FAKEIP_DNS_RULE_TAG = env("FAKEIP_DNS_RULE_TAG", "fakeip-dns-rule-tag");
    c.FAKEIP_RULESET_DNS_RULE_TAG = env("FAKEIP_RULESET_DNS_RULE_TAG", "fakeip-ruleset-dns-rule-tag");
    c.SERVICE_FAKEIP_DNS_RULE_TAG = env("SERVICE_FAKEIP_DNS_RULE_TAG", "service-fakeip-dns-rule-tag");

    c.DIRECT_OUTBOUND_TAG = env("DIRECT_OUTBOUND_TAG", "direct-out");
    c.BYPASS_OUTBOUND_TAG = env("BYPASS_OUTBOUND_TAG", "bypass-out");

    c.CLASH_API_CONTROLLER_PORT = env("CLASH_API_CONTROLLER_PORT", "9090");

    c.TMP_RULESET_FOLDER = env("TMP_RULESET_FOLDER", "/tmp/sing-box/rulesets");
    c.TMP_SING_BOX_FOLDER = env("TMP_SING_BOX_FOLDER", "/tmp/sing-box");
    c.TMP_SUBSCRIPTION_FOLDER = env("TMP_SUBSCRIPTION_FOLDER", "/tmp/sing-box/subscriptions");

    c.CHECK_PROXY_IP_DOMAIN = env("CHECK_PROXY_IP_DOMAIN", "ip.podkop.fyi");
    c.FAKEIP_TEST_DOMAIN = env("FAKEIP_TEST_DOMAIN", "fakeip.podkop.fyi");

    c.CLOUDFLARE_OCTETS = env("CLOUDFLARE_OCTETS", "8.47 162.159 188.114");

    c.SB_REQUIRED_VERSION = env("SB_REQUIRED_VERSION", "1.12.0");
    c.SB_MANAGED_SERVICE_MARKER = env("SB_MANAGED_SERVICE_MARKER", "Podkop managed sing-box service");
    c.SB_VARIANT_STATE_FILE = env("SB_VARIANT_STATE_FILE", "/etc/podkop/sing-box-variant");
    c.SB_VERSION_STATE_FILE = env("SB_VERSION_STATE_FILE", "/etc/podkop/sing-box-version");

    c.GITHUB_RAW_URL = env("GITHUB_RAW_URL", "https://raw.githubusercontent.com/itdoginfo/allow-domains/main");
    c.SRS_MAIN_URL = env("SRS_MAIN_URL", "https://github.com/itdoginfo/allow-domains/releases/latest/download");
    c.SRS_ADS_HAGEZI_PRO_URL = env("SRS_ADS_HAGEZI_PRO_URL", "https://github.com/zxc-rv/ad-filter/releases/latest/download/adlist.srs");
    c.SRS_SUPERCELL_URL = env("SRS_SUPERCELL_URL", "https://raw.githubusercontent.com/ushan0v/sing-box-supercell-ruleset/main/supercell.srs");

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

    c.COMMUNITY_SERVICES = env("COMMUNITY_SERVICES", "russia_inside russia_outside ukraine_inside geoblock block porn news anime youtube hdrezka tiktok google_ai google_play hodca discord meta twitter cloudflare cloudfront digitalocean hetzner ovh telegram roblox ads_hagezi_pro supercell github");

    c.RESERVED_TAGS = {
        [c.TPROXY_INBOUND_TAG]: true,
        [c.TPROXY_INBOUND6_TAG]: true,
        [c.DNS_INBOUND_TAG]: true,
        [c.SOURCE_DNS_INBOUND_TAG]: true,
        [c.DNS_SERVER_TAG]: true,
        [c.FAKEIP_DNS_SERVER_TAG]: true,
        [c.BOOTSTRAP_SERVER_TAG]: true,
        [c.FAKEIP_DNS_RULE_TAG]: true,
        [c.FAKEIP_RULESET_DNS_RULE_TAG]: true,
        [c.SERVICE_FAKEIP_DNS_RULE_TAG]: true,
        [c.DIRECT_OUTBOUND_TAG]: true,
        [c.BYPASS_OUTBOUND_TAG]: true,
    };

    c.URLTEST_DEFAULT_IDLE_TIMEOUT = env("URLTEST_DEFAULT_IDLE_TIMEOUT", "15m");
    c.DISABLED_UPDATE_INTERVAL = env("DISABLED_UPDATE_INTERVAL", "0");

    c.ZAPRET_PROVIDER_BASE_DIR = env("ZAPRET_PROVIDER_BASE_DIR", "/opt/zapret");
    c.ZAPRET_PROVIDER_NFQWS_BIN = env("ZAPRET_PROVIDER_NFQWS_BIN", c.ZAPRET_PROVIDER_BASE_DIR + "/nfq/nfqws");
    c.ZAPRET_PROVIDER_FILES_DIR = env("ZAPRET_PROVIDER_FILES_DIR", c.ZAPRET_PROVIDER_BASE_DIR + "/files");
    c.ZAPRET_PROVIDER_IPSET_DIR = env("ZAPRET_PROVIDER_IPSET_DIR", c.ZAPRET_PROVIDER_BASE_DIR + "/ipset");
    c.ZAPRET_LEGACY_RUNTIME_BASE_DIR = env("ZAPRET_LEGACY_RUNTIME_BASE_DIR", "/var/run/podkop/zapret-runtime");
    c.ZAPRET_NFQWS_BIN = env("ZAPRET_NFQWS_BIN", c.ZAPRET_PROVIDER_NFQWS_BIN);
    c.ZAPRET_STATE_DIR = env("ZAPRET_STATE_DIR", "/var/run/podkop/zapret");
    c.ZAPRET_PID_DIR = env("ZAPRET_PID_DIR", c.ZAPRET_STATE_DIR + "/pid");
    c.ZAPRET_CHILD_PID_DIR = env("ZAPRET_CHILD_PID_DIR", c.ZAPRET_STATE_DIR + "/child-pid");
    c.ZAPRET_LOG_DIR = env("ZAPRET_LOG_DIR", c.ZAPRET_STATE_DIR + "/log");
    c.ZAPRET_HOSTLIST_DIR = env("ZAPRET_HOSTLIST_DIR", c.ZAPRET_STATE_DIR + "/hostlist");
    c.ZAPRET_ROUTE_MARK_BASE = env("ZAPRET_ROUTE_MARK_BASE", "0x01000000");
    c.ZAPRET_QUEUE_BASE = env("ZAPRET_QUEUE_BASE", "4000");
    c.ZAPRET_QUEUE_RANGE_SIZE = env("ZAPRET_QUEUE_RANGE_SIZE", "256");
    c.ZAPRET_NFQWS_RESPAWN_DELAY = env("ZAPRET_NFQWS_RESPAWN_DELAY", "5");
    c.ZAPRET_DESYNC_MARK = env("ZAPRET_DESYNC_MARK", "0x40000000");
    c.ZAPRET_DESYNC_MARK_POSTNAT = env("ZAPRET_DESYNC_MARK_POSTNAT", "0x20000000");
    c.ZAPRET_LEGACY_DEFAULT_NFQWS_OPT = env("ZAPRET_LEGACY_DEFAULT_NFQWS_OPT", "--filter-tcp=80 <HOSTLIST> --dpi-desync=fake,fakedsplit --dpi-desync-autottl=2 --dpi-desync-fooling=badsum --new --filter-tcp=443 --dpi-desync=fake,multidisorder --dpi-desync-split-pos=1,midsld --dpi-desync-repeats=11 --dpi-desync-fooling=badsum --dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com --new --filter-udp=443 --hostlist=/opt/zapret/ipset/zapret-hosts-google.txt --dpi-desync=fake --dpi-desync-repeats=11 --dpi-desync-fake-quic=/opt/zapret/files/fake/quic_initial_www_google_com.bin --new --filter-udp=443 <HOSTLIST_NOAUTO> --dpi-desync=fake --dpi-desync-repeats=11 --new --filter-tcp=443 <HOSTLIST> --dpi-desync=multidisorder --dpi-desync-split-pos=1,sniext+1,host+1,midsld-2,midsld,midsld+2,endhost-1");
    c.ZAPRET_DEFAULT_NFQWS_OPT = env("ZAPRET_DEFAULT_NFQWS_OPT", "--filter-tcp=80 --dpi-desync=fake,fakedsplit --dpi-desync-autottl=2 --dpi-desync-fooling=badsum --new --filter-tcp=443 --dpi-desync=fake,multidisorder --dpi-desync-split-pos=1,midsld --dpi-desync-repeats=11 --dpi-desync-fooling=badsum --dpi-desync-fake-tls-mod=rnd,dupsid,sni=www.google.com --new --filter-udp=443 --dpi-desync=fake --dpi-desync-repeats=11 --dpi-desync-fake-quic=/opt/zapret/files/fake/quic_initial_www_google_com.bin");

    c.ZAPRET2_PROVIDER_BASE_DIR = env("ZAPRET2_PROVIDER_BASE_DIR", "/opt/zapret2");
    c.ZAPRET2_PROVIDER_NFQWS2_BIN = env("ZAPRET2_PROVIDER_NFQWS2_BIN", c.ZAPRET2_PROVIDER_BASE_DIR + "/nfq2/nfqws2");
    c.ZAPRET2_PROVIDER_FILES_DIR = env("ZAPRET2_PROVIDER_FILES_DIR", c.ZAPRET2_PROVIDER_BASE_DIR + "/files");
    c.ZAPRET2_PROVIDER_IPSET_DIR = env("ZAPRET2_PROVIDER_IPSET_DIR", c.ZAPRET2_PROVIDER_BASE_DIR + "/ipset");
    c.ZAPRET2_PROVIDER_LUA_DIR = env("ZAPRET2_PROVIDER_LUA_DIR", c.ZAPRET2_PROVIDER_BASE_DIR + "/lua");
    c.ZAPRET2_NFQWS2_BIN = env("ZAPRET2_NFQWS2_BIN", c.ZAPRET2_PROVIDER_NFQWS2_BIN);
    c.ZAPRET2_STATE_DIR = env("ZAPRET2_STATE_DIR", "/var/run/podkop/zapret2");
    c.ZAPRET2_PID_DIR = env("ZAPRET2_PID_DIR", c.ZAPRET2_STATE_DIR + "/pid");
    c.ZAPRET2_CHILD_PID_DIR = env("ZAPRET2_CHILD_PID_DIR", c.ZAPRET2_STATE_DIR + "/child-pid");
    c.ZAPRET2_LOG_DIR = env("ZAPRET2_LOG_DIR", c.ZAPRET2_STATE_DIR + "/log");
    c.ZAPRET2_ROUTE_MARK_BASE = env("ZAPRET2_ROUTE_MARK_BASE", "0x02000000");
    c.ZAPRET2_QUEUE_BASE = env("ZAPRET2_QUEUE_BASE", "4300");
    c.ZAPRET2_QUEUE_RANGE_SIZE = env("ZAPRET2_QUEUE_RANGE_SIZE", "256");
    c.ZAPRET2_NFQWS2_RESPAWN_DELAY = env("ZAPRET2_NFQWS2_RESPAWN_DELAY", "5");
    c.ZAPRET2_DESYNC_MARK = env("ZAPRET2_DESYNC_MARK", "0x40000000");
    c.ZAPRET2_DESYNC_MARK_POSTNAT = env("ZAPRET2_DESYNC_MARK_POSTNAT", "0x20000000");
    c.ZAPRET2_DEFAULT_NFQWS2_OPT = env("ZAPRET2_DEFAULT_NFQWS2_OPT", "--filter-tcp=80 --filter-l7=http --payload=http_req --lua-desync=fake:blob=fake_default_http:tcp_md5 --lua-desync=multisplit:pos=method+2 --new --filter-tcp=443 --filter-l7=tls --payload=tls_client_hello --lua-desync=fake:blob=fake_default_tls:tcp_md5:tcp_seq=-10000 --lua-desync=multidisorder:pos=1,midsld --new --filter-udp=443 --filter-l7=quic --payload=quic_initial --lua-desync=fake:blob=fake_default_quic:repeats=6");

    c.BYEDPI_BIN = env("BYEDPI_BIN", "/usr/bin/ciadpi");
    c.BYEDPI_SERVICE_INIT = env("BYEDPI_SERVICE_INIT", "/etc/init.d/byedpi");
    c.BYEDPI_STATE_DIR = env("BYEDPI_STATE_DIR", "/var/run/podkop/byedpi");
    c.BYEDPI_PID_DIR = env("BYEDPI_PID_DIR", c.BYEDPI_STATE_DIR + "/pid");
    c.BYEDPI_CHILD_PID_DIR = env("BYEDPI_CHILD_PID_DIR", c.BYEDPI_STATE_DIR + "/child-pid");
    c.BYEDPI_LOG_DIR = env("BYEDPI_LOG_DIR", c.BYEDPI_STATE_DIR + "/log");
    c.BYEDPI_LISTEN_ADDRESS = env("BYEDPI_LISTEN_ADDRESS", "127.0.0.1");
    c.BYEDPI_PORT_BASE = env("BYEDPI_PORT_BASE", "1080");
    c.BYEDPI_RESPAWN_DELAY = env("BYEDPI_RESPAWN_DELAY", "5");
    c.BYEDPI_OPEN_FILES_LIMIT = env("BYEDPI_OPEN_FILES_LIMIT", "4096");
    c.BYEDPI_DEFAULT_CMD_OPTS = env("BYEDPI_DEFAULT_CMD_OPTS", "-o 2 --auto=t,r,a,s -d 2");

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

    c.COREUTILS_BASE64_REQUIRED_VERSION = env("COREUTILS_BASE64_REQUIRED_VERSION", "9.7");

    function tag(base, postfix) {
        base = as_string(base);
        postfix = as_string(postfix);
        if (postfix == "") return base;
        return base + "-" + postfix;
    }

    function outbound_tag(section_name) {
        section_name = as_string(section_name);
        if (section_name == "") return "outbound";
        return "out-" + section_name;
    }

    c.tag = tag;
    c.outbound_tag = outbound_tag;

    return c;
}

function print_shell_env(constants) {
    for (let name in sort(keys(constants))) {
        let value = constants[name];
        if (type(value) != "function")
            print(name, "=", "'" + replace(as_string(value), /'/g, "'\\''") + "'", "\n");
    }
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