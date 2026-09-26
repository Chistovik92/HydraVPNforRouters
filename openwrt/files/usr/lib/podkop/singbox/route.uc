#!/usr/bin/env ucode

let constants = require("singbox.constants");
let common = require("core.common");
let connections = require("config.connections");
let rule_config = require("config.rule");
let runtime_rulesets = require("singbox.rulesets");

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

function config(settings, runtime_context) {
    runtime_context = object_or_empty(runtime_context);

    let rule_set = [];
    let rules = [];

    if (bool_option(settings, "geoip_enabled", true)) {
        push(rule_set, {
            type: "remote",
            tag: "builtin-geoip-ruleset",
            format: "binary",
            url: "https://github.com/SagerNet/sing-geoip/releases/latest/download/geoip.db",
            download_detour: constants.DIRECT_OUTBOUND_TAG,
            update_interval: "168h",
        });
    }

    if (bool_option(settings, "geosite_enabled", true)) {
        push(rule_set, {
            type: "remote",
            tag: "builtin-geosite-ruleset",
            format: "binary",
            url: "https://github.com/SagerNet/sing-geosite/releases/latest/download/geosite.db",
            download_detour: constants.DIRECT_OUTBOUND_TAG,
            update_interval: "168h",
        });
    }

    let auto_detect_interface = bool_option(settings, "auto_detect_interface", true);
    let interface = option(settings, "interface", "");
    let inbound_interface = option(settings, "inbound_interface", "");

    let route = {
        rule_set: rule_set,
        rules: rules,
        auto_detect_interface: auto_detect_interface,
        interface: interface,
        inbound_interface: inbound_interface,
        final: constants.DIRECT_OUTBOUND_TAG,
        override_android_vpn: false,
    };

    if (bool_option(settings, "bypass_private", true)) {
        push(rules, {
            type: "logical",
            mode: "or",
            rules: [
                { ip_cidr: ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10"] },
            ],
            outbound: constants.BYPASS_OUTBOUND_TAG,
        });
    }

    let bypass_ips = list_option(settings, "bypass_ip");
    if (length(bypass_ips) > 0) {
        push(rules, {
            ip_cidr: bypass_ips,
            outbound: constants.BYPASS_OUTBOUND_TAG,
        });
    }

    let bypass_domains = list_option(settings, "bypass_domain");
    if (length(bypass_domains) > 0) {
        push(rules, {
            domain_suffix: bypass_domains,
            outbound: constants.BYPASS_OUTBOUND_TAG,
        });
    }

    return route;
}

function module_exports() {
    return { config };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);