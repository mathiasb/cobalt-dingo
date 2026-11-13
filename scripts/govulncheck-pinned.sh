#!/usr/bin/env bash
# Run govulncheck under the Go version go.mod pins, as CI does (setup-go
# go-version-file). A newer local Go hides stdlib advisories fixed only in
# later patch releases of the pinned line (#132).
# govulncheck is built with the same toolchain: mixed toolchains give the
# "source-processing packages" skew error.
set -euo pipefail
gomod=${GOMOD:-go.mod}
ver=$(awk '$1=="go"{print $2; exit}' "$gomod")
[ -n "$ver" ] || { echo "no go directive in $gomod" >&2; exit 2; }
GOTOOLCHAIN="go${ver}" exec go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 "$@"
