#!/usr/bin/env ucode

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

function detect_server_country(outbound) {
    let remark = as_string(outbound.remark || "");
    let flag = match(remark, /[\u{1F1E6}-\u{1F1FF}]{2}/u);
    if (flag != null) {
        let c1 = flag.charCodeAt(0) - 0x1F1E6 + 65;
        let c2 = flag.charCodeAt(1) - 0x1F1E6 + 65;
        return chr(c1) + chr(c2);
    }
    return "";
}

function module_exports() {
    return { detect_server_country };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);