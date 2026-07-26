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
dummy_snp_report="${evidence_directory}/dummy-snp-report.txt"
trusted_root=$(mktemp)
trap 'rm -f "${trusted_root}"' EXIT

for required_command in cosign go jq openssl; do
  command -v "${required_command}" >/dev/null || {
    echo "${required_command} is required" >&2
    exit 1
  }
done

test -f "${bundle}" || { echo "missing ${bundle}" >&2; exit 1; }
test -f "${subject}" || { echo "missing ${subject}" >&2; exit 1; }

# When the test-only SNP input is present, prove that the values embedded in
# the signed subject were derived from that exact downloaded file. This checks
# data-flow integrity only; it is not AMD SEV-SNP attestation verification.
if [[ -f "${dummy_snp_report}" ]]; then
  report_sha256=$(openssl dgst -sha256 "${dummy_snp_report}" | awk '{print $NF}')
  measurement_sha384=$(openssl dgst -sha384 "${dummy_snp_report}" | awk '{print $NF}')

  jq -e \
    --arg report_sha256 "${report_sha256}" \
    --arg measurement "${measurement_sha384}" \
    '
      .snp.mode == "placeholder" and
      .snp.reportSha256 == $report_sha256 and
      .snp.measurement.algorithm == "sha384" and
      .snp.measurement.value == $measurement and
      .snp.verification.result == "test-only"
    ' "${subject}" >/dev/null

  echo "Placeholder SNP fields match dummy-snp-report.txt (test-only)"
fi

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
