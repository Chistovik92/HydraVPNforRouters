#!/usr/bin/env ucode

let uci = require("core.uci");
let common = require("core.common");
let constants = require("core.constants");

function as_string(value) {
    return value == null ? "" : "" + value;
}

function object_or_empty(value) {
    if (type(value) == "object") return value;
    return {};
}

function array_or_empty(value) {
    if (type(value) == "array") return value;
    return [];
}

function option(obj, key, fallback) {
    obj = object_or_empty(obj);
    let value = obj[key];
    return value == null ? as_string(fallback) : as_string(value);
}

function bool_option(obj, key, fallback) {
    obj = object_or_empty(obj);
    let value = obj[key];
    if (value === true || value === false) return value;
    if (value == 1 || value == "1" || value == "true" || value == "yes" || value == "on")
        return true;
    if (value == 0 || value == "0" || value == "false" || value == "no" || value == "off")
        return false;
    return fallback === true;
}

function section_name(section) {
    return as_string(object_or_empty(section)[".name"]);
}

function rule_sections() {
    let result = [];
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "section"))) {
        push(result, s);
    }
    return result;
}

function rule_enabled(section) {
    return bool_option(section, "enabled", true);
}

function rule_name(section) {
    return option(section, "name", section_name(section));
}

function rule_interface(section) {
    return option(section, "interface", "wan");
}

function rule_routing_mode(section) {
    return option(section, "routing_mode", "tproxy");
}

function rule_tun_device(section) {
    return option(section, "tun_device", "tun0");
}

function rule_mtu(section) {
    return int_option(section, "mtu", 1500);
}

function rule_dns_mode(section) {
    return option(section, "dns_mode", "fakeip");
}

function rule_dns_servers(section) {
    let servers = list_option(section, "dns_server");
    if (length(servers) == 0)
        servers = ["https://1.1.1.1/dns-query", "https://dns.google/dns-query"];
    return servers;
}

function rule_fakeip_ranges(section) {
    let ranges = list_option(section, "fakeip_range");
    if (length(ranges) == 0)
        ranges = [constants.SB_FAKEIP_INET4_RANGE, constants.SB_FAKEIP_INET6_RANGE];
    return ranges;
}

function rule_bypass_ips(section) {
    return list_option(section, "bypass_ip");
}

function rule_bypass_domains(section) {
    return list_option(section, "bypass_domain");
}

function rule_proxy_outbound(section) {
    return option(section, "proxy_outbound", "");
}

function rule_proxy_protocol(section) {
    return option(section, "proxy_protocol", "");
}

function rule_proxy_address(section) {
    return option(section, "proxy_address", "");
}

function rule_proxy_port(section) {
    return int_option(section, "proxy_port", 0);
}

function rule_proxy_uuid(section) {
    return option(section, "proxy_uuid", "");
}

function rule_proxy_password(section) {
    return option(section, "proxy_password", "");
}

function rule_proxy_security(section) {
    return option(section, "proxy_security", "auto");
}

function rule_proxy_sni(section) {
    return option(section, "proxy_sni", "");
}

function rule_proxy_alpn(section) {
    return option(section, "proxy_alpn", "");
}

function rule_proxy_fingerprint(section) {
    return option(section, "proxy_fingerprint", "");
}

function rule_proxy_flow(section) {
    return option(section, "proxy_flow", "");
}

function rule_proxy_transport(section) {
    return option(section, "proxy_transport", "tcp");
}

function rule_proxy_path(section) {
    return option(section, "proxy_path", "/");
}

function rule_proxy_host(section) {
    return option(section, "proxy_host", "");
}

function rule_proxy_service_name(section) {
    return option(section, "proxy_service_name", "");
}

function rule_proxy_reality_pbk(section) {
    return option(section, "proxy_reality_pbk", "");
}

function rule_proxy_reality_sid(section) {
    return option(section, "proxy_reality_sid", "");
}

function rule_proxy_reality_spx(section) {
    return option(section, "proxy_reality_spx", "/");
}

function rule_proxy_tcp_header_type(section) {
    return option(section, "proxy_tcp_header_type", "");
}

function rule_proxy_grpc_multi(section) {
    return bool_option(section, "proxy_grpc_multi", false);
}

function rule_proxy_allow_insecure(section) {
    return bool_option(section, "proxy_allow_insecure", false);
}

function rule_proxy_method(section) {
    return option(section, "proxy_method", "aes-256-gcm");
}

function rule_proxy_network(section) {
    return option(section, "proxy_network", "tcp");
}

function rule_proxy_aid(section) {
    return int_option(section, "proxy_aid", 0);
}

function module_exports() {
    return {
        rule_sections,
        rule_enabled,
        rule_name,
        rule_interface,
        rule_routing_mode,
        rule_tun_device,
        rule_mtu,
        rule_dns_mode,
        rule_dns_servers,
        rule_fakeip_ranges,
        rule_bypass_ips,
        rule_bypass_domains,
        rule_proxy_outbound,
        rule_proxy_protocol,
        rule_proxy_address,
        rule_proxy_port,
        rule_proxy_uuid,
        rule_proxy_password,
        rule_proxy_security,
        rule_proxy_sni,
        rule_proxy_alpn,
        rule_proxy_fingerprint,
        rule_proxy_flow,
        rule_proxy_transport,
        rule_proxy_path,
        rule_proxy_host,
        rule_proxy_service_name,
        rule_proxy_reality_pbk,
        rule_proxy_reality_sid,
        rule_proxy_reality_spx,
        rule_proxy_tcp_header_type,
        rule_proxy_grpc_multi,
        rule_proxy_allow_insecure,
        rule_proxy_method,
        rule_proxy_network,
        rule_proxy_aid,
    };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);