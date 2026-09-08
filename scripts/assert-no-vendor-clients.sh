#!/usr/bin/env bash
# Executable assertion for ADR-0002 (docs/adr/0002-ingest-files-not-vendor-integrations.md).
#
# Asserts the END STATE: financial sources without an official self-serve API
# are integrated by parsing files, never by calling the vendor. No unofficial
# Avanza / SEB / Kivra client has crept into the source tree.
#
# Same assertion shape infra ADR-0020 recorded as working for openbanking-api's
# "never call a bank" clause.
#
# Asserts on BEHAVIOUR — a vendor host in the source — never on vocabulary.
# ADR-0020 records a false positive where a "no kanban integration" assertion
# went red by matching the comments explaining why there was no kanban
# integration. docs/ is therefore excluded by construction: the ADRs discuss
# these vendors at length and would trip exactly that trap.
#
# Fortnox is deliberately NOT in the denylist. It has an official API and this
# repo is built on it.

set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

# Hosts that would indicate a vendor integration ADR-0002 rejected.
HOSTS='avanza\.se|sebkort\.com|seb\.se|kivra\.(se|com)'

fail=0
red()   { printf '  RED   %s\n' "$1"; fail=1; }
green() { printf '  GREEN %s\n' "$1"; }

echo "ADR-0002 — file ingestion, not vendor integrations"
echo

# Precondition: the ingest path this ADR mandates must exist. A missing
# precondition is RED, not "nothing to check" (ADR-0020 constraint 4).
# This is why the assertion is red today — nothing has been built yet.
if [ -d internal/statements ]; then
    green "internal/statements present"
else
    red "internal/statements absent — the Parse port ADR-0002 mandates does not exist yet"
fi

# The assertion proper: no vendor host in non-test Go source.
hits=$(grep -rInE "$HOSTS" --include='*.go' . 2>/dev/null \
    | grep -v '_test\.go:' \
    | grep -v '^\./docs/')

if [ -n "$hits" ]; then
    red "vendor host referenced in non-test source:"
    printf '%s\n' "$hits" | sed 's/^/        /'
else
    green "no Avanza / SEB / Kivra host in non-test source"
fi

echo
if [ $fail -ne 0 ]; then
    echo "ADR-0002 NOT IN FORCE"
    exit 1
fi
echo "ADR-0002 in force"
