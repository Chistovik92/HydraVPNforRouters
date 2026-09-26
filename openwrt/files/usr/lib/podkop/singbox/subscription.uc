#!/usr/bin/env ucode

let constants = require("singbox.constants");
let common = require("core.common");
let connections = require("config.connections");
let subscription_parser = require("subscription.parser");

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

function source_id(section_name, source_index) {
    return section_name + "-source-" + source_index;
}

function source_cache_path(source_section) {
    return constants.TMP_SUBSCRIPTION_FOLDER + "/" + source_section + ".json";
}

function source_cache_is_current(source_section, source_entry, user_agent, hwid) {
    let cache_path = source_cache_path(source_section);
    let cache = common.read_json_file(cache_path);
    if (object_or_empty(cache).version != 1)
        return false;
    if (as_string(cache.user_agent) != user_agent)
        return false;
    if (as_string(cache.hwid) != hwid)
        return false;
    if (cache.content_hash != hash_subscription(source_entry))
        return false;
    return true;
}

function hash_subscription(source_entry) {
    let url = option(source_entry, "url", "");
    let content = option(source_entry, "content", "");
    return hash(url + "|" + content);
}

function read_source_outbounds(source_section) {
    let cache_path = source_cache_path(source_section);
    let cache = common.read_json_file(cache_path);
    return array_or_empty(object_or_empty(cache).outbounds);
}

function update_subscription_cache(source_section, source_entry, outbounds, user_agent, hwid) {
    let cache_path = source_cache_path(source_section);
    common.ensure_dir(constants.TMP_SUBSCRIPTION_FOLDER);
    common.write_json_file(cache_path, {
        version: 1,
        user_agent: user_agent,
        hwid: hwid,
        content_hash: hash_subscription(source_entry),
        outbounds: outbounds,
        updated: clock()[0],
    });
}

function remember_outbound_metadata(state, tag, display_name, outbound) {
    state = object_or_empty(state);
    let metadata = object_or_empty(state.outboundMetadata);
    let names = object_or_empty(metadata.names);
    let countries = object_or_empty(metadata.countries);
    let protocols = object_or_empty(metadata.protocols);
    let transports = object_or_empty(metadata.transports);
    let securities = object_or_empty(metadata.securities);

    names[tag] = display_name;

    let country = detect_country(outbound);
    if (country != "") countries[tag] = uc(country);

    let proto = as_string(outbound.type || "");
    if (proto != "") protocols[tag] = proto;

    let transport = as_string(object_or_empty(outbound.transport).type || "");
    if (transport != "") transports[tag] = transport;

    let security = "none";
    if (proto == "vless" || proto == "vmess" || proto == "trojan" || proto == "shadowsocks") {
        let tls = object_or_empty(outbound.tls);
        if (bool_option(tls, "enabled", false)) {
            if (tls.reality != null) security = "reality";
            else security = "tls";
        }
    }
    securities[tag] = security;

    metadata.names = names;
    metadata.countries = countries;
    metadata.protocols = protocols;
    metadata.transports = transports;
    metadata.securities = securities;
    state.outboundMetadata = metadata;
}

function remember_source_outbound(state, tag, display_name, outbound, share_link) {
    remember_outbound_metadata(state, tag, display_name, outbound);
    state = object_or_empty(state);
    let sources = object_or_empty(state.subscriptionSources);
    sources[tag] = { shareLink: share_link };
    state.subscriptionSources = sources;
}

function remember_urltest_group(state, tag, display_name, outbound) {
    state = object_or_empty(state);
    let groups = object_or_empty(state.urltestGroups);
    groups[tag] = {
        displayName: display_name,
        outbounds: array_or_empty(outbound.outbounds),
        url: outbound.url,
        interval: outbound.interval,
        tolerance: outbound.tolerance,
        idle_timeout: outbound.idle_timeout,
        interrupt_exist_connections: outbound.interrupt_exist_connections,
    };
    state.urltestGroups = groups;
}

function remember_urltest_group_config(state, tag, config) {
    state = object_or_empty(state);
    let groups = object_or_empty(state.urltestGroups);
    groups[tag] = config;
    state.urltestGroups = groups;
}

function merge_source_metadata(state, section_name, source_section, source_index, source_entry) {
    state = object_or_empty(state);
    let sources = object_or_empty(state.subscriptionSourceInfo);
    let key = section_name + "-" + source_section;
    sources[key] = {
        section: section_name,
        source_section: source_section,
        index: source_index,
        url: option(source_entry, "url", ""),
        name: option(source_entry, "name", "Source " + (source_index + 1)),
    };
    state.subscriptionSourceInfo = sources;
}

function detect_country(outbound) {
    let remark = as_string(outbound.remark || "");
    let flag = match(remark, /[\u{1F1E6}-\u{1F1FF}]{2}/u);
    if (flag != null) return flag;

    let server = as_string(outbound.server || "");
    return "";
}

function module_exports() {
    return {
        source_id,
        source_cache_path,
        source_cache_is_current,
        read_source_outbounds,
        update_subscription_cache,
        remember_outbound_metadata,
        remember_source_outbound,
        remember_urltest_group,
        remember_urltest_group_config,
        merge_source_metadata,
        detect_country,
    };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);