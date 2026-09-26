#!/usr/bin/env ucode

let constants = require("singbox.constants");
let common = require("core.common");
let connections = require("config.connections");
let rule_config = require("config.rule");

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

function config(settings) {
    let dns_mode = option(settings, "dns_mode", "fakeip");
    let dns_servers = list_option(settings, "dns_server");
    if (length(dns_servers) == 0)
        dns_servers = ["https://1.1.1.1/dns-query", "https://dns.google/dns-query"];

    let servers = [];
    let rules = [];
    let inbounds = [];
    let sniff_inbounds = [];

    if (dns_mode == "fakeip") {
        let fakeip_ranges = list_option(settings, "fakeip_range");
        if (length(fakeip_ranges) == 0)
            fakeip_ranges = [constants.FAKEIP_INET4_RANGE, constants.FAKEIP_INET6_RANGE];

        push(servers, {
            type: "fakeip",
            tag: constants.FAKEIP_DNS_SERVER_TAG,
            inet4_range: fakeip_ranges[0],
            inet6_range: fakeip_ranges[1],
        });

        push(servers, {
            type: "remote",
            tag: constants.BOOTSTRAP_SERVER_TAG,
            address: dns_servers[0],
            dialer: constants.DIRECT_OUTBOUND_TAG,
        });

        push(rules, {
            action: "route",
            server: constants.FAKEIP_DNS_SERVER_TAG,
            domain: [constants.FAKEIP_TEST_DOMAIN, constants.CHECK_PROXY_IP_DOMAIN],
            rewrite_ttl: int_option(settings, "dns_rewrite_ttl", 60),
        });

        for (let server in dns_servers) {
            if (server == dns_servers[0]) continue;
            push(servers, {
                type: "remote",
                tag: "bootstrap-" + server,
                address: server,
                dialer: constants.DIRECT_OUTBOUND_TAG,
            });
        }
    } else {
        for (let server in dns_servers) {
            push(servers, {
                type: "remote",
                tag: "dns-" + server,
                address: server,
                dialer: constants.DIRECT_OUTBOUND_TAG,
            });
        }
    }

    push(inbounds, {
        type: "direct",
        tag: constants.DNS_INBOUND_TAG,
        listen: constants.DNS_INBOUND_ADDRESS,
        listen_port: constants.DNS_INBOUND_PORT,
    });

    return {
        unsupported: "",
        servers: servers,
        rules: rules,
        inbounds: inbounds,
        sniff_inbounds: sniff_inbounds,
    };
}

function default_domain_resolver(settings) {
    return option(settings, "default_domain_resolver", "dns-server");
}

function module_exports() {
    return { config, default_domain_resolver };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);