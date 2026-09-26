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

function community_url(reference) {
    if (reference == "russia_inside")
        return constants.SRS_MAIN_URL + "/RU_inside.srs";
    if (reference == "russia_outside")
        return constants.SRS_MAIN_URL + "/RU_outside.srs";
    if (reference == "ukraine_inside")
        return constants.SRS_MAIN_URL + "/UA_inside.srs";
    if (reference == "geoblock")
        return constants.SRS_MAIN_URL + "/geoblock.srs";
    if (reference == "block")
        return constants.SRS_MAIN_URL + "/block.srs";
    if (reference == "porn")
        return constants.SRS_MAIN_URL + "/porn.srs";
    if (reference == "news")
        return constants.SRS_MAIN_URL + "/news.srs";
    if (reference == "anime")
        return constants.SRS_MAIN_URL + "/anime.srs";
    if (reference == "youtube")
        return constants.SRS_MAIN_URL + "/youtube.srs";
    if (reference == "hdrezka")
        return constants.SRS_MAIN_URL + "/hdrezka.srs";
    if (reference == "tiktok")
        return constants.SRS_MAIN_URL + "/tiktok.srs";
    if (reference == "google_ai")
        return constants.SRS_MAIN_URL + "/google_ai.srs";
    if (reference == "google_play")
        return constants.SRS_MAIN_URL + "/google_play.srs";
    if (reference == "hodca")
        return constants.SRS_MAIN_URL + "/hodca.srs";
    if (reference == "discord")
        return constants.SUBNETS_DISCORD;
    if (reference == "meta")
        return constants.SUBNETS_META;
    if (reference == "twitter")
        return constants.SUBNETS_TWITTER;
    if (reference == "cloudflare")
        return constants.SUBNETS_CLOUDFLARE;
    if (reference == "cloudfront")
        return constants.SUBNETS_CLOUDFRONT;
    if (reference == "digitalocean")
        return constants.SUBNETS_DIGITALOCEAN;
    if (reference == "hetzner")
        return constants.SUBNETS_HETZNER;
    if (reference == "ovh")
        return constants.SUBNETS_OVH;
    if (reference == "telegram")
        return constants.SUBNETS_TELEGRAM;
    if (reference == "roblox")
        return constants.SUBNETS_ROBLOX;
    if (reference == "ads_hagezi_pro")
        return constants.SRS_ADS_HAGEZI_PRO_URL;
    if (reference == "supercell")
        return constants.SRS_SUPERCELL_URL;
    if (reference == "github")
        return constants.GITHUB_RAW_URL + "/Subnets/IPv4/github.lst";
    return "";
}

function is_community(reference) {
    reference = as_string(reference);
    let services = split(as_string(constants.COMMUNITY_SERVICES), " ");
    for (let s in services)
        if (s == reference) return true;
    return false;
}

function kind_from_reference_hint(reference) {
    reference = as_string(reference);
    if (is_community(reference)) return "domains";
    if (match(reference, /\.(srs|json)$/) != null) return "domains";
    if (match(reference, /\.(lst|txt)$/) != null) return "ip_cidr";
    return "unknown";
}

function file_extension(reference) {
    reference = as_string(reference);
    let dot = rindex(reference, ".");
    if (dot >= 0) return substr(reference, dot + 1);
    return "";
}

function remote_format(reference) {
    reference = as_string(reference);
    if (is_community(reference)) return "binary";
    let ext = file_extension(reference);
    if (ext == "srs") return "binary";
    if (ext == "json") return "source";
    if (ext == "lst" || ext == "txt") return "text";
    return "source";
}

function hash12(str) {
    str = as_string(str);
    let hash = 0;
    for (let i = 0; i < str.length; i++)
        hash = ((hash << 5) - hash + str.charCodeAt(i)) | 0;
    return (hash >>> 0).toString(16).substr(0, 12);
}

function module_exports() {
    return {
        community_url,
        is_community,
        kind_from_reference_hint,
        file_extension,
        remote_format,
        hash12,
    };
}

if (sourcepath(1) != null && sourcepath(1) != "")
    return module_exports();

exit(0);