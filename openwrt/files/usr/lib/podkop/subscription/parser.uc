#!/usr/bin/env ucode

let uci = require("core.uci");
let common = require("core.common");
let constants = require("core.constants");
let connections = require("config.connections");

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

function section_name(section) {
    return as_string(object_or_empty(section)[".name"]);
}

function subscription_sources(section) {
    let result = [];
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "subscription_url"))) {
        if (option(s, "section", "") == section_name(section))
            push(result, s);
    }
    return result;
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

function parse_subscription_content(content, source_entry) {
    content = as_string(content);
    if (content == "") return [];

    let outbounds = [];

    let lines = split(content, "\n");
    for (let line in lines) {
        line = trim(line);
        if (line == "" || substr(line, 0, 1) == "#") continue;

        let parsed = parse_proxy_url(line);
        if (parsed != null) {
            parsed.__forkop_source = "subscription";
            push(outbounds, parsed);
        }
    }

    return outbounds;
}

function parse_proxy_url(url) {
    url = as_string(url);
    if (url == "") return null;

    let scheme_end = index(url, "://");
    if (scheme_end < 0) return null;
    let proto = substr(url, 0, scheme_end);
    let rest = substr(url, scheme_end + 3);

    let at = rindex(rest, "@");
    let userinfo = "";
    let hostport = rest;
    if (at >= 0) {
        userinfo = substr(rest, 0, at);
        hostport = substr(rest, at + 1);
    }

    let hash = index(hostport, "#");
    let remark = "";
    if (hash >= 0) {
        remark = decode(substr(hostport, hash + 1));
        hostport = substr(hostport, 0, hash);
    }

    let question = index(hostport, "?");
    let query = {};
    if (question >= 0) {
        let query_str = substr(hostport, question + 1);
        hostport = substr(hostport, 0, question);
        let pairs = split(query_str, "&");
        for (let pair in pairs) {
            let eq = index(pair, "=");
            if (eq > 0) {
                let key = decode(substr(pair, 0, eq));
                let value = decode(substr(pair, eq + 1));
                query[key] = value;
            }
        }
    }

    let host = hostport;
    let port = 0;
    let bracket_end = index(hostport, "]");
    if (substr(hostport, 0, 1) == "[" && bracket_end >= 0) {
        host = substr(hostport, 1, bracket_end - 1);
        let after = substr(hostport, bracket_end + 1);
        if (substr(after, 0, 1) == ":")
            port = int(substr(after, 1), 10);
    } else {
        let colon = rindex(hostport, ":");
        if (colon >= 0) {
            host = substr(hostport, 0, colon);
            port = int(substr(hostport, colon + 1), 10);
        }
    }

    let uuid = "";
    let password = "";
    if (userinfo != "") {
        let colon = index(userinfo, ":");
        if (colon >= 0) {
            uuid = substr(userinfo, 0, colon);
            password = substr(userinfo, colon + 1);
        } else {
            uuid = userinfo;
        }
    }

    let outbound = {
        share_link: url,
        remark: remark,
    };

    if (proto == "vless") {
        outbound.type = "vless";
        outbound.server = host;
        outbound.server_port = port;
        outbound.uuid = uuid;
        outbound.flow = query["flow"] || "";
        outbound.tls = build_tls(query);
        outbound.transport = build_transport(query);
    } else if (proto == "vmess") {
        outbound.type = "vmess";
        outbound.server = host;
        outbound.server_port = port;
        outbound.uuid = uuid;
        outbound.alter_id = int(query["aid"] || "0", 10);
        outbound.security = query["scy"] || "auto";
        outbound.tls = build_tls(query);
        outbound.transport = build_transport(query);
    } else if (proto == "trojan") {
        outbound.type = "trojan";
        outbound.server = host;
        outbound.server_port = port;
        outbound.password = uuid;
        outbound.tls = build_tls(query);
        outbound.transport = build_transport(query);
    } else if (proto == "shadowsocks" || proto == "ss") {
        outbound.type = "shadowsocks";
        outbound.server = host;
        outbound.server_port = port;
        outbound.method = query["method"] || "aes-256-gcm";
        outbound.password = password || uuid;
        outbound.tls = build_tls(query);
        outbound.transport = build_transport(query);
    } else if (proto == "socks" || proto == "socks5") {
        outbound.type = "socks";
        outbound.server = host;
        outbound.server_port = port;
        outbound.username = uuid;
        outbound.password = password;
        outbound.tls = build_tls(query);
        outbound.transport = build_transport(query);
    } else if (proto == "hysteria2" || proto == "hy2") {
        outbound.type = "hysteria2";
        outbound.server = host;
        outbound.server_port = port;
        outbound.password = uuid;
        outbound.tls = build_tls(query);
        outbound.obfs = build_hysteria2_obfs(query);
    } else if (proto == "wireguard" || proto == "wg") {
        outbound.type = "wireguard";
        outbound.server = host;
        outbound.server_port = port;
        outbound.private_key = query["privateKey"] || "";
        outbound.peer_public_key = query["publicKey"] || "";
        outbound.pre_shared_key = query["presharedKey"] || "";
        outbound.reserved = query["reserved"] ? split(query["reserved"], ",") : [];
        outbound.mtu = int(query["mtu"] || "1280", 10);
    } else {
        return null;
    }

    return outbound;
}

function build_tls(query) {
    let security = query["security"] || "none";
    if (security == "none") return { enabled: false };

    let tls = { enabled: true };
    if (query["sni"] != null) tls.server_name = query["sni"];
    if (query["alpn"] != null) tls.alpn = split(query["alpn"], ",");
    if (query["fp"] != null) tls.fingerprint = query["fp"];
    if (query["allowInsecure"] != null) tls.insecure = query["allowInsecure"] == "1";

    if (security == "reality") {
        tls.reality = {
            public_key: query["pbk"] || "",
            short_id: query["sid"] || "",
            spider_x: query["spx"] || "/",
        };
    }

    return tls;
}

function build_transport(query) {
    let net = query["type"] || "tcp";
    let transport = { type: net };

    if (net == "ws") {
        transport.path = query["path"] || "/";
        transport.headers = { Host: query["host"] || "" };
    } else if (net == "grpc") {
        transport.service_name = query["serviceName"] || "";
        transport.multi_mode = query["multiMode"] == "1";
    } else if (net == "http") {
        transport.path = query["path"] || "/";
        transport.host = query["host"] ? [query["host"]] : [];
    } else if (net == "httpupgrade" || net == "h2") {
        transport.path = query["path"] || "/";
        transport.host = query["host"] || "";
    } else if (net == "tcp") {
        transport.header = { type: query["headerType"] || "none" };
    }

    return transport;
}

function build_hysteria2_obfs(query) {
    let obfs = query["obfs"] || "none";
    if (obfs == "none") return null;
    if (obfs == "salamander") {
        return { type: "salamander", password: query["obfsPassword"] || "" };
    }
    return null;
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

function is_copyable_link(url) {
    url = as_string(url);
    let proto = scheme(url);
    return proto == "vless" || proto == "vmess" || proto == "trojan" ||
           proto == "shadowsocks" || proto == "ss" || proto == "socks" ||
           proto == "hysteria2" || proto == "hy2" || proto == "wireguard" || proto == "wg";
}

function serialize_outbound_link(outbound) {
    let proto = as_string(outbound.type || "");
    if (proto == "") return "";

    let server = as_string(outbound.server || "");
    let port = int(outbound.server_port || 0);
    let uuid = as_string(outbound.uuid || "");
    let password = as_string(outbound.password || "");
    let remark = as_string(outbound.remark || "");

    let base = proto + "://";
    if (uuid != "" || password != "") {
        base += uuid;
        if (password != "") base += ":" + password;
        base += "@";
    }
    base += server + ":" + port;

    let params = [];
    if (proto == "vless") {
        let flow = as_string(outbound.flow || "");
        if (flow != "") push(params, "flow=" + flow);
    } else if (proto == "vmess") {
        push(params, "aid=" + int(outbound.alter_id || 0));
        push(params, "scy=" + as_string(outbound.security || "auto"));
    } else if (proto == "trojan") {
    } else if (proto == "shadowsocks") {
        push(params, "method=" + as_string(outbound.method || "aes-256-gcm"));
    }

    let tls = object_or_empty(outbound.tls);
    if (bool_option(tls, "enabled", false)) {
        push(params, "security=tls");
        let sni = as_string(tls.server_name || "");
        if (sni != "") push(params, "sni=" + encode(sni));
        let alpn = array_or_empty(tls.alpn);
        if (length(alpn) > 0) push(params, "alpn=" + encode(join(",", alpn)));
        let fp = as_string(tls.fingerprint || "");
        if (fp != "") push(params, "fp=" + encode(fp));
        if (bool_option(tls, "insecure", false)) push(params, "allowInsecure=1");

        let reality = object_or_empty(tls.reality);
        if (reality.public_key != null) {
            params[params.length - 1] = "security=reality";
            push(params, "pbk=" + encode(as_string(reality.public_key)));
            push(params, "sid=" + encode(as_string(reality.short_id)));
            push(params, "spx=" + encode(as_string(reality.spider_x)));
        }
    }

    let transport = object_or_empty(outbound.transport);
    let net = as_string(transport.type || "tcp");
    push(params, "type=" + net);

    if (net == "ws") {
        push(params, "path=" + encode(as_string(transport.path || "/")));
        let host = as_string(transport.headers.Host || "");
        if (host != "") push(params, "host=" + encode(host));
    } else if (net == "grpc") {
        push(params, "serviceName=" + encode(as_string(transport.service_name || "")));
        if (bool_option(transport, "multi_mode", false)) push(params, "multiMode=1");
    } else if (net == "http") {
        push(params, "path=" + encode(as_string(transport.path || "/")));
        let hosts = array_or_empty(transport.host);
        if (length(hosts) > 0) push(params, "host=" + encode(hosts[0]));
    } else if (net == "httpupgrade" || net == "h2") {
        push(params, "path=" + encode(as_string(transport.path || "/")));
        let host = as_string(transport.host || "");
        if (host != "") push(params, "host=" + encode(host));
    } else if (net == "tcp") {
        let header = as_string(object_or_empty(transport.header).type || "none");
        if (header != "none") push(params, "headerType=" + header);
    }

    if (remark != "") base += "#" + encode(remark);

    if (length(params) > 0) base += "?" + join("&", params);

    return base;
}

function module_exports() {
    return {
        subscription_sources,
        source_cache_path,
        source_cache_is_current,
        read_source_outbounds,
        parse_subscription_content,
        parse_proxy_url,
        update_subscription_cache,
        remember_outbound_metadata,
        remember_source_outbound,
        remember_urltest_group,
        remember_urltest_group_config,
        merge_source_metadata,
        is_copyable_link,
        serialize_outbound_link,
    };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);