#!/usr/bin/env bash
# Observes: govulncheck-pinned.sh runs `go` with GOTOOLCHAIN set to go.mod's
# version and passes govulncheck's exit status through. Uses a fake `go`.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
printf 'module x\n\ngo 1.26.10\n' > "$tmp/go.mod"
cat > "$tmp/go" <<'F'
#!/usr/bin/env bash
echo "GOTOOLCHAIN=$GOTOOLCHAIN ARGS=$*" > "$FAKE_GO_LOG"
exit "${FAKE_GO_EXIT:-0}"
F
chmod +x "$tmp/go"
fail=0
FAKE_GO_LOG="$tmp/log" PATH="$tmp:$PATH" GOMOD="$tmp/go.mod" "$here/govulncheck-pinned.sh" ./... >/dev/null
grep -q 'GOTOOLCHAIN=go1.26.10 ' "$tmp/log" || { echo "FAIL: GOTOOLCHAIN not set from go.mod"; fail=1; }
grep -q 'govulncheck@' "$tmp/log" || { echo "FAIL: govulncheck not invoked via go run"; fail=1; }
if FAKE_GO_EXIT=3 FAKE_GO_LOG="$tmp/log" PATH="$tmp:$PATH" GOMOD="$tmp/go.mod" "$here/govulncheck-pinned.sh" ./... >/dev/null 2>&1; then
  echo "FAIL: non-zero exit swallowed"; fail=1
fi
[ "$fail" -eq 0 ] && echo "ok"
exit "$fail"
