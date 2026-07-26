#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 EVIDENCE_DIRECTORY [EXPECTED_CERTIFICATE_IDENTITY]" >&2
  exit 2
fi

evidence_directory=$1
expected_identity=${2:-"https://github.com/tkgsn/sigstore-public-good-instance-test/.github/workflows/rekor-v2.yml@refs/heads/main"}
script_directory=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
bundle="${evidence_directory}/rekor-v2-test.sigstore.json"
subject="${evidence_directory}/rekor-v2-test-subject.json"
trusted_root=$(mktemp)
trap 'rm -f "${trusted_root}"' EXIT

for required_command in cosign go jq; do
  command -v "${required_command}" >/dev/null || {
    echo "${required_command} is required" >&2
    exit 1
  }
done

test -f "${bundle}" || { echo "missing ${bundle}" >&2; exit 1; }
test -f "${subject}" || { echo "missing ${subject}" >&2; exit 1; }

# This obtains the TrustedRoot through Sigstore's TUF repository.
cosign trusted-root create \
  --with-default-services \
  --out "${trusted_root}"

# Cosign verifies the artifact signature, Fulcio chain, GitHub OIDC identity,
# RFC 3161 timestamp, checkpoint signature, and transparency-log proof.
cosign verify-blob \
  --bundle "${bundle}" \
  --trusted-root "${trusted_root}" \
  --certificate-identity "${expected_identity}" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  "${subject}"

# Independently recompute the RFC 6962 Merkle root and verify the C2SP
# checkpoint's Ed25519 signature with the matching TUF TrustedRoot key.
go run "${script_directory}/verify_rekor_v2_bundle.go" \
  --bundle "${bundle}" \
  --trusted-root "${trusted_root}"
