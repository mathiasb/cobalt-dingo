#!/usr/bin/env bash
# Executable assertion for ADR-0004.
#
# End state: exactly one package serves authenticated HTML, and the receipts
# HTTP server registers nothing but /health. Catches a second, unauthenticated
# front end reappearing — which is how the current one arrived, via a merge
# rather than a decision.
#
# Fails closed on missing preconditions (ADR-0020 constraint 4).

set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

fail=0
red()   { printf '  RED   %s\n' "$1"; fail=1; }
green() { printf '  GREEN %s\n' "$1"; }

echo "ADR-0004 — one authenticated front end"
echo

RECEIPTS_UI="internal/receipts/httpserver/ui"

# 1. The receipts admin UI package must be gone. Its existence is the condition
#    this ADR removes, so its presence is red regardless of how it is mounted.
if [ -d "$RECEIPTS_UI" ]; then
    red "$RECEIPTS_UI still exists — the second front end has not been folded in"
else
    green "$RECEIPTS_UI is gone"
fi

# 2. The receipts server must register no route other than /health. Asserted on
#    the route strings actually registered, not on package names.
SERVER="internal/receipts/httpserver/server.go"
if [ ! -f "$SERVER" ]; then
    green "$SERVER is gone entirely (acceptable end state — see ADR-0004 consequences)"
else
    routes=$(grep -oE '(HandleFunc|Handle)\("[^"]+"' "$SERVER" | sed 's/.*("//; s/"$//' | sort -u)
    if [ -z "$routes" ]; then
        red "no routes parsed from $SERVER — the registration shape changed; re-point this assertion"
    else
        extra=$(printf '%s\n' "$routes" | grep -vE '^(GET )?/health$' || true)
        if [ -n "$extra" ]; then
            red "$SERVER registers routes beyond /health:"
            printf '%s\n' "$extra" | sed 's/^/        /'
        else
            green "$SERVER registers only /health"
        fi
    fi
fi

# 3. Every registered UI route must sit behind auth. Existence-checked
#    separately from passing, for the same reason as ADR-0003's check.
T="TestAllUIRoutesRequireAuth"
listed=$(go test -list "$T" ./internal/ui/ 2>/dev/null)
if ! printf '%s\n' "$listed" | grep -qx "$T"; then
    red "$T does not exist — nothing asserts that new routes inherit auth"
elif go test ./internal/ui/ -run "$T" -count=1 >/dev/null 2>&1; then
    green "$T passes"
else
    red "$T fails"
fi

echo
if [ $fail -ne 0 ]; then
    echo "ADR-0004 NOT IN FORCE"
    exit 1
fi
echo "ADR-0004 in force"
