#!/bin/sh
# HydraVPN for Router - update script
#
# Thin wrapper around install.sh --update, so installation and update share
# one implementation (platform detection, asset names, version comparison).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/update.sh | sh
#   sh update.sh --yes           # non-interactive (cron)
#   sh update.sh --check-only    # only report
#   sh update.sh --force         # reinstall the current version
#
# All options are passed to install.sh (see install.sh --help).

set -eu

INSTALL_URL="https://raw.githubusercontent.com/Chistovik92/HydraVPNforRouters/main/scripts/install.sh"

SCRIPT_DIR=$(cd "$(dirname "$0")" 2>/dev/null && pwd) || SCRIPT_DIR=""
if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/install.sh" ]; then
    exec sh "$SCRIPT_DIR/install.sh" --update "$@"
fi

TMP=$(mktemp /tmp/hydravpn-install.XXXXXX)
trap 'rm -f "$TMP"' EXIT INT TERM

if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$TMP" "$INSTALL_URL"
elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$TMP" "$INSTALL_URL"
else
    uclient-fetch -q -O "$TMP" "$INSTALL_URL"
fi

sh "$TMP" --update "$@"
