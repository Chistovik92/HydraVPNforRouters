#!/usr/bin/env ucode

let constants = require("singbox.constants");
let common = require("core.common");

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

function lc(str) {
    return as_string(str).toLowerCase();
}

function uc(str) {
    return as_string(str).toUpperCase();
}

function countries_from_flag_names(names) {
    names = object_or_empty(names);
    let countries = {};
    for (let tag, name in names) {
        let flag = match(as_string(name), /[\u{1F1E6}-\u{1F1FF}]{2}/u);
        if (flag != null) {
            let c1 = flag.charCodeAt(0) - 0x1F1E6 + 65;
            let c2 = flag.charCodeAt(1) - 0x1F1E6 + 65;
            countries[tag] = chr(c1) + chr(c2);
        }
    }
    return countries;
}

function normalized_country_list(list) {
    let result = [];
    for (let c in array_or_empty(list)) {
        let cc = uc(as_string(c));
        if (cc != "") push(result, cc);
    }
    return result;
}

function regex_matching_tag_array(tags, names, regexes) {
    let result = [];
    for (let tag in array_or_empty(tags)) {
        let name = tag_display_name(tag, names);
        for (let regex in array_or_empty(regexes)) {
            if (match(name, regex) != null) {
                push(result, tag);
                break;
            }
        }
    }
    return result;
}

function tag_display_name(tag, names) {
    let name = as_string(object_or_empty(names)[tag] || "");
    return name != "" ? name : tag;
}

function module_exports() {
    return {
        countries_from_flag_names,
        normalized_country_list,
        regex_matching_tag_array,
        tag_display_name,
    };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);