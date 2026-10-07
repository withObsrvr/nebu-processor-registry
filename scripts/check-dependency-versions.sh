#!/usr/bin/env bash
set -euo pipefail

expected_sdk="${EXPECTED_STELLAR_SDK_VERSION:-v0.7.3}"
expected_nebu="${EXPECTED_NEBU_VERSION:-v0.6.14}"
failed=0

for mod in processors/*/go.mod; do
    processor="$(basename "$(dirname "$mod")")"
    sdk_version="$(awk '$1 == "github.com/stellar/go-stellar-sdk" { print $2 } $1 == "require" && $2 == "github.com/stellar/go-stellar-sdk" { print $3 }' "$mod")"
    nebu_version="$(awk '$1 == "github.com/withObsrvr/nebu" { print $2 } $1 == "require" && $2 == "github.com/withObsrvr/nebu" { print $3 }' "$mod")"

    if [[ "$sdk_version" != "$expected_sdk" ]]; then
        echo "ERROR: $processor uses go-stellar-sdk ${sdk_version:-<missing>}; expected $expected_sdk" >&2
        failed=1
    fi
    if [[ "$nebu_version" != "$expected_nebu" ]]; then
        echo "ERROR: $processor uses nebu ${nebu_version:-<missing>}; expected $expected_nebu" >&2
        failed=1
    fi
done

if ((failed)); then
    exit 1
fi

echo "All processor modules use go-stellar-sdk $expected_sdk and nebu $expected_nebu"
