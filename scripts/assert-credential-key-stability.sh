#!/usr/bin/env bash
# Executable assertion for ADR-0003.
#
# End state: no stored credential is keyed by the OIDC subject, so an IdP change
# cannot orphan one. Asserts the outcome, not that a mechanism once worked.
#
# Fails closed on every precondition (ADR-0020 constraint 4). Capture before
# matching — never `cmd | grep -q` under pipefail, which reports failure on a
# successful match because grep -q exits early and the producer takes SIGPIPE.

set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

fail=0
red()   { printf '  RED   %s\n' "$1"; fail=1; }
green() { printf '  GREEN %s\n' "$1"; }

echo "ADR-0003 — credentials are not keyed by the OIDC subject"
echo

# 1. The credential key must not be derived from the subject. Asserted on the
#    expression that builds it, not on the word "sub" appearing anywhere.
if [ ! -f internal/auth/session.go ]; then
    red "internal/auth/session.go missing — cannot evaluate, failing closed"
else
    body=$(awk '/func \(s Session\) TenantID\(\)/,/^}/' internal/auth/session.go)
    if [ -z "$body" ]; then
        red "Session.TenantID() not found — the credential key moved; re-point this assertion"
    elif printf '%s\n' "$body" | grep -qE '\bs\.Sub\b'; then
        red "Session.TenantID() still derives the credential key from the OIDC subject"
    else
        green "the credential key is not derived from the OIDC subject"
    fi
fi

# 2. The regression test must exist AND pass. `go test -run X` exits 0 when
#    nothing matches X, so existence is checked separately — otherwise this
#    reports green precisely when nothing is being asserted.
T="TestCredentialOwner_SurvivesSubjectChange"
listed=$(go test -list "$T" ./internal/auth/ 2>/dev/null)
if ! printf '%s\n' "$listed" | grep -qx "$T"; then
    red "$T does not exist — the Dex→Authentik failure is not reproduced anywhere"
elif go test ./internal/auth/ -run "$T" -count=1 >/dev/null 2>&1; then
    green "$T passes"
else
    red "$T fails"
fi

echo
if [ $fail -ne 0 ]; then
    echo "ADR-0003 NOT IN FORCE"
    exit 1
fi
echo "ADR-0003 in force"
