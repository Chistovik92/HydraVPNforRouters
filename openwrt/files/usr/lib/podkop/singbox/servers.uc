#!/usr/bin/env ucode

let constants = require("singbox.constants");
let common = require("core.common");
let connections = require("config.connections");
let rule_config = require("config.rule");
let subscription = require("subscription.parser");

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

function list_option(obj, key) {
    obj = object_or_empty(obj);
    let value = obj[key];
    if (type(value) == "array") return value;
    if (value == null) return [];
    return [as_string(value)];
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

function int_option(obj, key, fallback) {
    obj = object_or_empty(obj);
    let value = obj[key];
    if (type(value) == "number") return int(value);
    if (type(value) == "string" && match(value, /^[0-9]+$/))
        return int(value, 10);
    return int(as_string(fallback), 10);
}

function server_sections() {
    let result = [];
    for (let s in array_or_empty(require("core.uci").section_objects(constants.PODKOP_CONFIG_NAME, "server"))) {
        push(result, s);
    }
    return result;
}

function server_enabled(section) {
    return bool_option(section, "enabled", true);
}

function server_name(section) {
    return option(section, "name", require("core.common").section_name(section));
}

function server_protocol(section) {
    return option(section, "protocol", "vless");
}

function server_address(section) {
    return option(section, "address", "");
}

function server_port(section) {
    return int_option(section, "port", 443);
}

function server_uuid(section) {
    return option(section, "uuid", "");
}

function server_password(section) {
    return option(section, "password", "");
}

function server_security(section) {
    return option(section, "security", "tls");
}

function server_sni(section) {
    return option(section, "sni", "");
}

function server_alpn(section) {
    return option(section, "alpn", "h2,http/1.1");
}

function server_fingerprint(section) {
    return option(section, "fingerprint", "chrome");
}

function server_flow(section) {
    return option(section, "flow", "");
}

function server_transport(section) {
    return option(section, "transport", "tcp");
}

function server_path(section) {
    return option(section, "path", "/");
}

function server_host(section) {
    return option(section, "host", "");
}

function server_service_name(section) {
    return option(section, "service_name", "");
}

function server_reality_pbk(section) {
    return option(section, "reality_pbk", "");
}

function server_reality_sid(section) {
    return option(section, "reality_sid", "");
}

function server_reality_spx(section) {
    return option(section, "reality_spx", "/");
}

function server_tcp_header_type(section) {
    return option(section, "tcp_header_type", "");
}

function server_grpc_multi(section) {
    return bool_option(section, "grpc_multi", false);
}

function server_allow_insecure(section) {
    return bool_option(section, "allow_insecure", false);
}

function server_method(section) {
    return option(section, "method", "aes-256-gcm");
}

function server_network(section) {
    return option(section, "network", "tcp");
}

function server_aid(section) {
    return int_option(section, "aid", 0);
}

function server_remark(section) {
    return option(section, "remark", "");
}

function build_server_outbound(section, tag_prefix) {
    let protocol = server_protocol(section);
    let tag = tag_prefix || constants.outbound_tag(require("core.common").section_name(section));

    let outbound = {
        type: protocol,
        tag: tag,
    };

    if (protocol == "vless") {
        outbound.server = server_address(section);
        outbound.server_port = server_port(section);
        outbound.uuid = server_uuid(section);
        outbound.flow = server_flow(section);
        outbound.tls = build_tls(section);
        outbound.transport = build_transport(section);
    } else if (protocol == "vmess") {
        outbound.server = server_address(section);
        outbound.server_port = server_port(section);
        outbound.uuid = server_uuid(section);
        outbound.alter_id = server_aid(section);
        outbound.security = server_security(section);
        outbound.tls = build_tls(section);
        outbound.transport = build_transport(section);
    } else if (protocol == "trojan") {
        outbound.server = server_address(section);
        outbound.server_port = server_port(section);
        outbound.password = server_password(section);
        outbound.tls = build_tls(section);
        outbound.transport = build_transport(section);
    } else if (protocol == "shadowsocks") {
        outbound.server = server_address(section);
        outbound.server_port = server_port(section);
        outbound.method = server_method(section);
        outbound.password = server_password(section);
        outbound.tls = build_tls(section);
        outbound.transport = build_transport(section);
    } else if (protocol == "socks") {
        outbound.server = server_address(section);
        outbound.server_port = server_port(section);
        outbound.username = server_uuid(section);
        outbound.password = server_password(section);
        outbound.tls = build_tls(section);
        outbound.transport = build_transport(section);
    } else if (protocol == "hysteria2") {
        outbound.server = server_address(section);
        outbound.server_port = server_port(section);
        outbound.password = server_password(section);
        outbound.tls = build_tls(section);
        outbound.obfs = build_hysteria2_obfs(section);
    } else if (protocol == "wireguard") {
        outbound.server = server_address(section);
        outbound.server_port = server_port(section);
        outbound.private_key = option(section, "private_key", "");
        outbound.peer_public_key = option(section, "peer_public_key", "");
        outbound.pre_shared_key = option(section, "pre_shared_key", "");
        outbound.reserved = list_option(section, "reserved");
        outbound.mtu = int_option(section, "mtu", 1280);
    } else {
        return null;
    }

    let remark = server_remark(section);
    if (remark != "") outbound.remark = remark;

    return outbound;
}

function build_tls(section) {
    let security = server_security(section);
    if (security == "none") return { enabled: false };

    let tls = { enabled: true };
    let sni = server_sni(section);
    if (sni != "") tls.server_name = sni;
    else tls.server_name = server_address(section);

    let alpn = server_alpn(section);
    if (alpn != "") tls.alpn = split(alpn, ",");

    let fp = server_fingerprint(section);
    if (fp != "") tls.fingerprint = fp;

    if (server_allow_insecure(section)) tls.insecure = true;

    if (security == "reality") {
        tls.reality = {
            public_key: server_reality_pbk(section),
            short_id: server_reality_sid(section),
            spider_x: server_reality_spx(section),
        };
    }

    return tls;
}

function build_transport(section) {
    let net = server_transport(section);
    let transport = { type: net };

    if (net == "ws") {
        transport.path = server_path(section);
        let host = server_host(section);
        if (host != "") transport.headers = { Host: host };
    } else if (net == "grpc") {
        transport.service_name = server_service_name(section);
        transport.multi_mode = server_grpc_multi(section);
    } else if (net == "http") {
        transport.path = server_path(section);
        let host = server_host(section);
        if (host != "") transport.host = [host];
    } else if (net == "httpupgrade" || net == "h2") {
        transport.path = server_path(section);
        let host = server_host(section);
        if (host != "") transport.host = host;
    } else if (net == "tcp") {
        let header = server_tcp_header_type(section);
        if (header != "") transport.header = { type: header };
    }

    return transport;
}

function build_hysteria2_obfs(section) {
    let obfs = option(section, "obfs", "none");
    if (obfs == "none") return null;
    if (obfs == "salamander") {
        return { type: "salamander", password: option(section, "obfs_password", "") };
    }
    return null;
}

function module_exports() {
    return {
        server_sections,
        server_enabled,
        server_name,
        server_protocol,
        server_address,
        server_port,
        server_uuid,
        server_password,
        server_security,
        server_sni,
        server_alpn,
        server_fingerprint,
        server_flow,
        server_transport,
        server_path,
        server_host,
        server_service_name,
        server_reality_pbk,
        server_reality_sid,
        server_reality_spx,
        server_tcp_header_type,
        server_grpc_multi,
        server_allow_insecure,
        server_method,
        server_network,
        server_aid,
        server_remark,
        build_server_outbound,
    };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);