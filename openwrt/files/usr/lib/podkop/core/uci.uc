#!/usr/bin/env ucode

let fs = require("fs");

function as_string(value) {
    return value == null ? "" : "" + value;
}

function load(package_name) {
    package_name = as_string(package_name);
    let config_path = "/etc/config/" + package_name;
    if (fs.stat(config_path) == null) return false;
    return true;
}

function get_all(package_name, section_name) {
    package_name = as_string(package_name);
    section_name = as_string(section_name);
    let config_path = "/etc/config/" + package_name;
    let content = fs.read(config_path);
    if (content == null) return {};

    let sections = parse_uci(content);
    for (let section in sections) {
        if (as_string(section[".name"]) == section_name ||
            as_string(section.type) == section_name) {
            return section;
        }
    }
    return {};
}

function section_objects(package_name, type_name) {
    package_name = as_string(package_name);
    type_name = as_string(type_name);
    let config_path = "/etc/config/" + package_name;
    let content = fs.read(config_path);
    if (content == null) return [];

    let sections = parse_uci(content);
    let result = [];
    for (let section in sections) {
        if (as_string(section.type) == type_name)
            push(result, section);
    }
    return result;
}

function parse_uci(content) {
    let lines = split(content, "\n");
    let sections = [];
    let current_section = null;

    for (let line in lines) {
        line = trim(line);
        if (line == "" || substr(line, 0, 1) == "#") continue;

        if (substr(line, 0, 7) == "config ") {
            if (current_section != null)
                push(sections, current_section);
            let parts = split(line, " ");
            current_section = {
                type: parts[1] || "",
                ".name": parts[2] ? trim(parts[2], "'\"") : "",
            };
        } else if (substr(line, 0, 6) == "option " && current_section != null) {
            let rest = substr(line, 7);
            let eq = index(rest, " ");
            if (eq > 0) {
                let key = trim(substr(rest, 0, eq));
                let value = trim(substr(rest, eq + 1), "'\"");
                current_section[key] = value;
            }
        } else if (substr(line, 0, 4) == "list " && current_section != null) {
            let rest = substr(line, 5);
            let eq = index(rest, " ");
            if (eq > 0) {
                let key = trim(substr(rest, 0, eq));
                let value = trim(substr(rest, eq + 1), "'\"");
                if (type(current_section[key]) != "array")
                    current_section[key] = [];
                push(current_section[key], value);
            }
        }
    }

    if (current_section != null)
        push(sections, current_section);

    return sections;
}

function module_exports() {
    return { load, get_all, section_objects };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);