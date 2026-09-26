#!/usr/bin/env ucode

let uci = require("core.uci");
let common = require("core.common");
let constants = require("core.constants");

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

function section_name(section) {
    return as_string(object_or_empty(section)[".name"]);
}

function subscription_download_targets(sections) {
    let targets = [];
    for (let section in array_or_empty(sections)) {
        if (as_string(section.type) != "section")
            continue;
        if (!bool_option(section, "enabled", true))
            continue;
        let url = option(section, "url", "");
        if (url != "")
            push(targets, { section: section_name(section), url: url });
    }
    return targets;
}

function subscription_user_agent(section, source_entry) {
    let ua = option(section, "user_agent", "");
    if (ua != "") return ua;
    return option(source_entry, "user_agent", "sing-box/" + constants.SB_REQUIRED_VERSION);
}

function subscription_hwid(section, source_entry) {
    return option(source_entry, "hwid", "");
}

function source_id(section_name, source_index) {
    return section_name + "-source-" + source_index;
}

function urltests(section) {
    let result = [];
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "urltest"))) {
        if (option(s, "section", "") == section_name(section))
            push(result, section_name(s));
    }
    return result;
}

function urltest_settings(section, urltest_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "urltest"))) {
        if (option(s, "section", "") == section_name(section) &&
            section_name(s) == urltest_id)
            return s;
    }
    return {};
}

function urltest_display_name(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return option(s, "name", urltest_id);
}

function urltest_check_interval(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return option(s, "interval", "3m");
}

function urltest_tolerance(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return int_option(s, "tolerance", 50);
}

function urltest_interrupt_exist_connections(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return bool_option(s, "interrupt_exist_connections", false);
}

function urltest_filter_mode(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return option(s, "filter_mode", "disabled");
}

function urltest_include_outbounds(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return list_option(s, "include_outbound");
}

function urltest_include_regex(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return list_option(s, "include_regex");
}

function urltest_include_countries(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return list_option(s, "include_country");
}

function urltest_include_proxy_parameters(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return bool_option(s, "include_proxy_parameters", false);
}

function urltest_include_protocols(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return list_option(s, "include_protocol");
}

function urltest_include_transports(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return list_option(s, "include_transport");
}

function urltest_include_securities(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return list_option(s, "include_security");
}

function urltest_exclude_outbounds(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return list_option(s, "exclude_outbound");
}

function urltest_exclude_regex(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return list_option(s, "exclude_regex");
}

function urltest_exclude_countries(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return list_option(s, "exclude_country");
}

function urltest_exclude_proxy_parameters(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return bool_option(s, "exclude_proxy_parameters", false);
}

function urltest_exclude_protocols(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return list_option(s, "exclude_protocol");
}

function urltest_exclude_transports(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return list_option(s, "exclude_transport");
}

function urltest_exclude_securities(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return list_option(s, "exclude_security");
}

function urltest_testing_url(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return option(s, "testing_url", "http://cp.cloudflare.com/generate_204");
}

function urltest_idle_timeout(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return option(s, "idle_timeout", "");
}

function urltest_detect_server_country(section, urltest_id) {
    let s = urltest_settings(section, urltest_id);
    return option(s, "detect_server_country", "country_is");
}

function dashboard_filter_mode(section) {
    return option(section, "dashboard_filter_mode", "disabled");
}

function dashboard_include_outbounds(section) {
    return list_option(section, "dashboard_include_outbound");
}

function dashboard_include_regex(section) {
    return list_option(section, "dashboard_include_regex");
}

function dashboard_include_countries(section) {
    return list_option(section, "dashboard_include_country");
}

function dashboard_include_proxy_parameters(section) {
    return bool_option(section, "dashboard_include_proxy_parameters", false);
}

function dashboard_include_protocols(section) {
    return list_option(section, "dashboard_include_protocol");
}

function dashboard_include_transports(section) {
    return list_option(section, "dashboard_include_transport");
}

function dashboard_include_securities(section) {
    return list_option(section, "dashboard_include_security");
}

function dashboard_exclude_outbounds(section) {
    return list_option(section, "dashboard_exclude_outbound");
}

function dashboard_exclude_regex(section) {
    return list_option(section, "dashboard_exclude_regex");
}

function dashboard_exclude_countries(section) {
    return list_option(section, "dashboard_exclude_country");
}

function dashboard_exclude_proxy_parameters(section) {
    return bool_option(section, "dashboard_exclude_proxy_parameters", false);
}

function dashboard_exclude_protocols(section) {
    return list_option(section, "dashboard_exclude_protocol");
}

function dashboard_exclude_transports(section) {
    return list_option(section, "dashboard_exclude_transport");
}

function dashboard_exclude_securities(section) {
    return list_option(section, "dashboard_exclude_security");
}

function dashboard_detect_server_country(section) {
    return option(section, "dashboard_detect_server_country", "country_is");
}

function dashboard_include_groups(section) {
    return list_option(section, "dashboard_include_group");
}

function dashboard_exclude_groups(section) {
    return list_option(section, "dashboard_exclude_group");
}

function priority_groups(section) {
    let result = [];
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_group"))) {
        if (option(s, "section", "") == section_name(section))
            push(result, section_name(s));
    }
    return result;
}

function priority_group_display_name(section, group_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_group"))) {
        if (option(s, "section", "") == section_name(section) &&
            section_name(s) == group_id)
            return option(s, "name", group_id);
    }
    return group_id;
}

function priority_levels(group_id) {
    let result = [];
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id)
            push(result, section_name(s));
    }
    return result;
}

function priority_level_display_name(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return option(s, "name", level_id);
    }
    return level_id;
}

function priority_level_order(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return int_option(s, "order", 1);
    }
    return 1;
}

function priority_level_direct(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return bool_option(s, "direct", false);
    }
    return false;
}

function priority_level_filter_mode(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return option(s, "filter_mode", "disabled");
    }
    return "disabled";
}

function priority_level_include_outbounds(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return list_option(s, "include_outbound");
    }
    return [];
}

function priority_level_include_regex(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return list_option(s, "include_regex");
    }
    return [];
}

function priority_level_include_countries(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return list_option(s, "include_country");
    }
    return [];
}

function priority_level_include_proxy_parameters(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return bool_option(s, "include_proxy_parameters", false);
    }
    return false;
}

function priority_level_include_protocols(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return list_option(s, "include_protocol");
    }
    return [];
}

function priority_level_include_transports(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return list_option(s, "include_transport");
    }
    return [];
}

function priority_level_include_securities(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return list_option(s, "include_security");
    }
    return [];
}

function priority_level_exclude_outbounds(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return list_option(s, "exclude_outbound");
    }
    return [];
}

function priority_level_exclude_regex(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return list_option(s, "exclude_regex");
    }
    return [];
}

function priority_level_exclude_countries(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return list_option(s, "exclude_country");
    }
    return [];
}

function priority_level_exclude_proxy_parameters(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return bool_option(s, "exclude_proxy_parameters", false);
    }
    return false;
}

function priority_level_exclude_protocols(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return list_option(s, "exclude_protocol");
    }
    return [];
}

function priority_level_exclude_transports(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return list_option(s, "exclude_transport");
    }
    return [];
}

function priority_level_exclude_securities(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return list_option(s, "exclude_security");
    }
    return [];
}

function priority_level_detect_server_country(group_id, level_id) {
    for (let s in array_or_empty(uci.section_objects(constants.PODKOP_CONFIG_NAME, "priority_level"))) {
        if (option(s, "group", "") == group_id && section_name(s) == level_id)
            return option(s, "detect_server_country", "country_is");
    }
    return "country_is";
}

function section_interface(section) {
    return option(section, "interface", "wan");
}

function module_exports() {
    return {
        subscription_download_targets,
        subscription_user_agent,
        subscription_hwid,
        source_id,
        urltests,
        urltest_settings,
        urltest_display_name,
        urltest_check_interval,
        urltest_tolerance,
        urltest_interrupt_exist_connections,
        urltest_filter_mode,
        urltest_include_outbounds,
        urltest_include_regex,
        urltest_include_countries,
        urltest_include_proxy_parameters,
        urltest_include_protocols,
        urltest_include_transports,
        urltest_include_securities,
        urltest_exclude_outbounds,
        urltest_exclude_regex,
        urltest_exclude_countries,
        urltest_exclude_proxy_parameters,
        urltest_exclude_protocols,
        urltest_exclude_transports,
        urltest_exclude_securities,
        urltest_testing_url,
        urltest_idle_timeout,
        urltest_detect_server_country,
        dashboard_filter_mode,
        dashboard_include_outbounds,
        dashboard_include_regex,
        dashboard_include_countries,
        dashboard_include_proxy_parameters,
        dashboard_include_protocols,
        dashboard_include_transports,
        dashboard_include_securities,
        dashboard_exclude_outbounds,
        dashboard_exclude_regex,
        dashboard_exclude_countries,
        dashboard_exclude_proxy_parameters,
        dashboard_exclude_protocols,
        dashboard_exclude_transports,
        dashboard_exclude_securities,
        dashboard_detect_server_country,
        dashboard_include_groups,
        dashboard_exclude_groups,
        priority_groups,
        priority_group_display_name,
        priority_levels,
        priority_level_display_name,
        priority_level_order,
        priority_level_direct,
        priority_level_filter_mode,
        priority_level_include_outbounds,
        priority_level_include_regex,
        priority_level_include_countries,
        priority_level_include_proxy_parameters,
        priority_level_include_protocols,
        priority_level_include_transports,
        priority_level_include_securities,
        priority_level_exclude_outbounds,
        priority_level_exclude_regex,
        priority_level_exclude_countries,
        priority_level_exclude_proxy_parameters,
        priority_level_exclude_protocols,
        priority_level_exclude_transports,
        priority_level_exclude_securities,
        priority_level_detect_server_country,
        section_interface,
    };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);