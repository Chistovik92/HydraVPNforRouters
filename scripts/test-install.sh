#!/bin/sh
# Tests version_gt from install.sh (releases and -debug.N pre-releases).
#   sh scripts/test-install.sh
set -u
DIR=$(cd "$(dirname "$0")" && pwd)
# load only the two functions; install.sh runs main at the end
eval "$(sed -n '/^ident_gt() {/,/^}/p;/^version_gt() {/,/^}/p' "$DIR/install.sh")"

fail=0
check() { # expected(gt|le) A B
    if version_gt "$2" "$3"; then got=gt; else got=le; fi
    if [ "$got" != "$1" ]; then echo "FAIL: version_gt $2 $3 = $got, want $1"; fail=1; fi
}
check gt 1.2.2 1.2.1
check le 1.2.1 1.2.1
check le 1.2.0 1.2.1
check gt 1.10.0 1.9.9
check gt 1.2.2-debug.1 1.2.1
check le 1.2.2-debug.1 1.2.2
check gt 1.2.2 1.2.2-debug.9
check gt 1.2.2-debug.2 1.2.2-debug.1
check gt 1.2.2-debug.10 1.2.2-debug.9
check le 1.2.2-debug.1 1.2.2-debug.1
check le 1.2.1 1.2.2-debug.1
check gt 1.2.2-debug.1.1 1.2.2-debug.1
check gt 1.2.2-rc.1 1.2.2-debug.5
[ "$fail" = 0 ] && echo "ok: install.sh version comparison"
exit "$fail"
