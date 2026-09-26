#!/usr/bin/env ucode

function as_string(value) {
    return value == null ? "" : "" + value;
}

function decode(str) {
    str = as_string(str);
    return replace(str, /%([0-9A-Fa-f]{2})/g, function(_, hex) {
        return chr(int(hex, 16));
    });
}

function scheme(url) {
    url = as_string(url);
    let colon = index(url, "://");
    return colon > 0 ? substr(url, 0, colon) : "";
}

function fragment(url) {
    url = as_string(url);
    let hash = index(url, "#");
    return hash >= 0 ? substr(url, hash + 1) : "";
}

function strip_fragment(url) {
    url = as_string(url);
    let hash = index(url, "#");
    return hash >= 0 ? substr(url, 0, hash) : url;
}

function host(url) {
    url = as_string(url);
    let proto_end = index(url, "://");
    if (proto_end >= 0) url = substr(url, proto_end + 3);
    let at = rindex(url, "@");
    if (at >= 0) url = substr(url, at + 1);
    let slash = index(url, "/");
    let colon = index(url, ":");
    let end = url.length;
    if (slash >= 0) end = slash;
    if (colon >= 0 && colon < end) end = colon;
    return substr(url, 0, end);
}

function port(url) {
    url = as_string(url);
    let proto_end = index(url, "://");
    if (proto_end >= 0) url = substr(url, proto_end + 3);
    let at = rindex(url, "@");
    if (at >= 0) url = substr(url, at + 1);
    let colon = index(url, ":");
    let slash = index(url, "/");
    if (colon >= 0 && (slash < 0 || colon < slash)) {
        let end = slash >= 0 ? slash : url.length;
        let p = substr(url, colon + 1, end - colon - 1);
        return int(p, 10);
    }
    return 0;
}

function userinfo(url) {
    url = as_string(url);
    let proto_end = index(url, "://");
    if (proto_end >= 0) url = substr(url, proto_end + 3);
    let at = rindex(url, "@");
    if (at >= 0) return substr(url, 0, at);
    return "";
}

function path(url) {
    url = as_string(url);
    let proto_end = index(url, "://");
    if (proto_end >= 0) url = substr(url, proto_end + 3);
    let at = rindex(url, "@");
    if (at >= 0) url = substr(url, at + 1);
    let slash = index(url, "/");
    let hash = index(url, "#");
    let question = index(url, "?");
    let end = url.length;
    if (hash >= 0) end = hash;
    if (question >= 0 && question < end) end = question;
    return slash >= 0 ? substr(url, slash, end - slash) : "/";
}

function query_params(url) {
    url = as_string(url);
    let question = index(url, "?");
    let hash = index(url, "#");
    let query = "";
    if (question >= 0) {
        let end = hash >= 0 ? hash : url.length;
        query = substr(url, question + 1, end - question - 1);
    }
    let params = {};
    if (query == "") return params;
    let pairs = split(query, "&");
    for (let pair in pairs) {
        let eq = index(pair, "=");
        if (eq > 0) {
            let key = decode(substr(pair, 0, eq));
            let value = decode(substr(pair, eq + 1));
            params[key] = value;
        } else {
            params[decode(pair)] = "";
        }
    }
    return params;
}

function module_exports() {
    return { decode, scheme, fragment, strip_fragment, host, port, userinfo, path, query_params };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);