#!/usr/bin/env bash
# Executable assertion for ADR-0001 (docs/adr/0001-read-only-live-financial-data.md).
#
# Asserts the decision's END STATE: cobalt-dingo reads live company data and
# cannot write to it. Exits non-zero when the decision is not in force.
#
# Per infra ADR-0020 this asserts the outcome, not the mechanism. It is RED
# until live production OAuth actually happens (issue #50), and goes RED again
# if anyone enables writes. An assertion that passed before the decision was in
# force would be the wrong assertion.
#
# Every check FAILS CLOSED: a precondition it cannot evaluate is red, never
# "nothing to check" (ADR-0020 constraint 4).
#
# SECRET SAFETY: this script never prints a variable's value. The deployed
# configuration is inspected by variable NAME only.

set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

NAMESPACE="${COBALT_NAMESPACE:-cobalt-dingo}"
DEPLOYMENT="${COBALT_DEPLOYMENT:-cobalt-dingo}"
WRITE_FLAG="FORTNOX_PRODUCTION_ALLOW_WRITES"
TOKEN_FILE=".fortnox-tokens-production.json"

fail=0
red()   { printf '  RED   %s\n' "$1"; fail=1; }
green() { printf '  GREEN %s\n' "$1"; }

echo "ADR-0001 — live company data is read-only"
echo

# 1. A production token exists, i.e. the decision is actually in force and not
#    merely intended. Local file store first; postgres store is the deployed
#    case and is only assertable from a host with DATABASE_URL set.
if [ -f "$TOKEN_FILE" ]; then
    green "production token present ($TOKEN_FILE)"
elif [ -n "${DATABASE_URL:-}" ] && command -v psql >/dev/null 2>&1; then
    if psql "$DATABASE_URL" -tAc \
        "select 1 from fortnox_tokens where mode='production' limit 1" 2>/dev/null | grep -q 1; then
        green "production token present (postgres token store)"
    else
        red "no production token in the postgres token store — live OAuth never performed (#50)"
    fi
else
    red "cannot establish that a production token exists: no $TOKEN_FILE, and no DATABASE_URL+psql to check the postgres store"
fi

# 2. Writes are not enabled anywhere in the deployed configuration. ADR-0001
#    says the flag is set NOWHERE, so the variable's mere presence is red —
#    which also means we never need to read its value.
if command -v kubectl >/dev/null 2>&1; then
    names=$(kubectl get deploy "$DEPLOYMENT" -n "$NAMESPACE" \
        -o jsonpath='{.spec.template.spec.containers[*].env[*].name}' 2>/dev/null)
    rc=$?
    if [ $rc -ne 0 ]; then
        red "cannot read deployment $NAMESPACE/$DEPLOYMENT — precondition unmet, failing closed"
    elif printf '%s' "$names" | tr ' ' '\n' | grep -qx "$WRITE_FLAG"; then
        red "$WRITE_FLAG is present in the deployed configuration — ADR-0001 says it is set nowhere"
    else
        green "$WRITE_FLAG absent from the deployed configuration"
    fi
else
    red "kubectl unavailable — cannot assert the deployed configuration, failing closed"
fi

# 3. The wiring test holds: no production-mode code path yields a write-capable
#    client. This is the check that catches a ninth adapter wired wrong.
#
#    `go test -run X` exits 0 when NOTHING matches X, so running it is not
#    enough — that is a vacuous gate, and this script shipped with one for
#    about ten minutes. Assert the test EXISTS first, then that it passes.
#
#    Capture before matching, never `go test ... | grep -q`: grep -q exits on
#    the first match, go test takes SIGPIPE (141), and `set -o pipefail`
#    reports the whole pipeline as failed even though the test was found. That
#    inverted this very check on first run.
WIRING_TEST="TestProductionWiring_NeverYieldsWritableClient"
listed=$(go test -list "$WIRING_TEST" ./internal/adapter/fortnox/ 2>/dev/null)
if ! printf '%s\n' "$listed" | grep -qx "$WIRING_TEST"; then
    red "$WIRING_TEST does not exist — nothing asserts the wiring, failing closed"
elif go test ./internal/adapter/fortnox/ -run "$WIRING_TEST" -count=1 >/dev/null 2>&1; then
    green "$WIRING_TEST passes"
else
    red "$WIRING_TEST fails"
fi

echo
if [ $fail -ne 0 ]; then
    echo "ADR-0001 NOT IN FORCE"
    exit 1
fi
echo "ADR-0001 in force"
