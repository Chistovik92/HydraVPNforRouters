#!/usr/bin/env ucode

let fs = require("fs");

function as_string(value) {
    return value == null ? "" : "" + value;
}

function read_json_file(path) {
    path = as_string(path);
    let stat = fs.stat(path);
    if (stat == null) return {};
    let content = fs.read(path);
    if (content == null) return {};
    return parse_json(content) || {};
}

function read_stdin() {
    let content = fs.read("/dev/stdin");
    return content == null ? "" : content;
}

function read_stdin_json() {
    return parse_json(read_stdin()) || {};
}

function write_json(value) {
    return sprintf("%J", value);
}

function write_json_file(path, value) {
    path = as_string(path);
    let content = write_json(value);
    return fs.write(path, content) || false;
}

function csv_to_json_array(csv) {
    csv = as_string(csv);
    if (csv == "") return [];
    let parts = split(csv, ",");
    let result = [];
    for (let part in parts)
        push(result, trim(part));
    return result;
}

function strip_internal_fields(obj) {
    if (type(obj) != "object") return obj;
    let result = {};
    for (let key, value in obj) {
        if (substr(key, 0, 2) == "__") continue;
        result[key] = value;
    }
    return result;
}

function array_or_empty(value) {
    if (type(value) == "array") return value;
    return [];
}

function object_or_empty(value) {
    if (type(value) == "object") return value;
    return {};
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

function module_exports() {
    return {
        as_string,
        read_json_file,
        read_stdin,
        read_stdin_json,
        write_json,
        write_json_file,
        csv_to_json_array,
        strip_internal_fields,
        array_or_empty,
        object_or_empty,
        option,
        list_option,
        bool_option,
        int_option,
    };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);