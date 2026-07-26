#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 RUN_ID [OUTPUT_DIRECTORY]" >&2
  exit 2
fi

run_id=$1
output_directory=${2:-"evidence-${run_id}"}
repository="tkgsn/sigstore-public-good-instance-test"

command -v gh >/dev/null || {
  echo "gh is required: https://cli.github.com/" >&2
  exit 1
}

gh auth status --hostname github.com >/dev/null

artifact_names=()
while IFS= read -r artifact_name; do
  artifact_names+=("${artifact_name}")
done < <(
  gh api "repos/${repository}/actions/runs/${run_id}/artifacts" \
    --jq '.artifacts[] | select(.expired == false) | select(.name | startswith("rekor-v2-evidence-")) | .name'
)

if [[ ${#artifact_names[@]} -ne 1 ]]; then
  echo "expected exactly one non-expired rekor-v2-evidence artifact; found ${#artifact_names[@]}" >&2
  exit 1
fi

mkdir -p "${output_directory}"
gh run download "${run_id}" \
  --repo "${repository}" \
  --name "${artifact_names[0]}" \
  --dir "${output_directory}"

echo "downloaded ${artifact_names[0]} to ${output_directory}"
