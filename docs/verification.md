# Rekor v2 evidence verification

This document shows how to download a GitHub Actions artifact and verify its
placeholder SNP fields, Sigstore bundle, Rekor v2 inclusion proof, checkpoint
signature, and Merkle root.

## 1. Sign in to GitHub and download the artifact

### GitHub CLI

Install the [GitHub CLI](https://cli.github.com/), then sign in:

```shell
gh auth login --hostname github.com --git-protocol ssh --web
gh auth status --hostname github.com
```

Download the evidence from run `30187731233`:

```shell
./scripts/download_evidence.sh 30187731233 evidence-30187731233
```

The equivalent direct command is:

```shell
gh run download 30187731233 \
  --repo tkgsn/sigstore-public-good-instance-test \
  --name rekor-v2-evidence-30187731233-1 \
  --dir evidence-30187731233
```

### GitHub web UI

1. Sign in to GitHub.
2. Open [Actions run 30187731233](https://github.com/tkgsn/sigstore-public-good-instance-test/actions/runs/30187731233).
3. In the **Artifacts** section, select `rekor-v2-evidence-30187731233-1`.
4. Extract the downloaded ZIP file.

GitHub requires authentication even when downloading Actions artifacts from a
public repository. The GitHub Artifact API reported artifact ID `8627547700`,
archive digest
`sha256:58418cbd1741e8c4ed86fed4576d9d785fc2c71d87c97791457c67d586df8dd0`,
and expiration at `2026-08-25T04:24:42Z`.

The four extracted files are also committed byte-for-byte at
[`evidence/run-30187731233`](../evidence/run-30187731233). They are public
verification material and contain no GitHub credential or private signing key.

## 2. Downloaded files and how they are related

| Downloaded file | Role in this test |
| --- | --- |
| [`dummy-snp-report.txt`](../evidence/run-30187731233/dummy-snp-report.txt) | A clearly marked synthetic input. Its SHA-256 and SHA-384 are placed in the signed subject to exercise the data flow. It is not an AMD SEV-SNP attestation report. |
| [`rekor-v2-test-subject.json`](../evidence/run-30187731233/rekor-v2-test-subject.json) | The artifact actually signed. It identifies commit `ebc8552ffa6afc2fa4396a21615da315204c62ec`, workflow run `30187731233`, and contains the placeholder `snp` object. |
| [`rekor-v2-test.sigstore.json`](../evidence/run-30187731233/rekor-v2-test.sigstore.json) | The Sigstore bundle returned by signing. It contains the Fulcio certificate, artifact signature and digest, RFC 3161 timestamp, canonicalized Rekor v2 entry, Merkle inclusion proof, root hash, and signed checkpoint. |
| [`signing-config.json`](../evidence/run-30187731233/signing-config.json) | The TUF-distributed service configuration used by Cosign. Its `rekorTlogUrls` selects `https://log2025-1.rekor.sigstore.dev` with `majorApiVersion: 2`. It records service selection, but is not cryptographic proof of inclusion. |

The relationship is:

```text
dummy-snp-report.txt
  -> SHA-256 + SHA-384 placeholder values
  -> snp object in rekor-v2-test-subject.json

rekor-v2-test-subject.json
  -> SHA-256 digest + keyless signature
  -> canonicalized hashedrekord 0.0.2 entry in rekor-v2-test.sigstore.json
  -> RFC 6962 leaf hash + audit path
  -> root hash in the signed Rekor checkpoint

signing-config.json
  -> tells Cosign to submit the entry to the Rekor v2 Public Good Instance
```

The actual signed subject contains:

```json
{
  "snp": {
    "mode": "placeholder",
    "reportSha256": "cf0dd6d26c8a9be7e156532b1a0c684ee8e4bd1a6595ce031ef936577319c741",
    "measurement": {
      "algorithm": "sha384",
      "value": "951b09434b6674c0ea5d2b20512ce008513f05b02dfe66f54e81ecc0ca134801181b638e8c0d9dcf1d2568a37247c5d8"
    },
    "verification": {
      "result": "test-only",
      "note": "Not verified: generated from a dummy report for workflow testing."
    }
  }
}
```

`placeholder` and `test-only` are essential: these checks prove only that the
dummy file's hashes were included in the signed JSON. They do not establish an
AMD certificate chain, SNP report signature, TCB status, policy, nonce, or
genuine SNP measurement.

For this subject, the base64-encoded SHA-256 digest in
`messageSignature.messageDigest.digest` is:

```text
F79C/73jpvhd28HLODuQckyKHru2BxZvnae/yZyD5hI=
```

Rekor stores that digest, its signature, and the Fulcio certificate in the
bundle's base64-encoded `canonicalizedBody`. It does not store the subject JSON
body or interpret the `snp` fields separately.

## 3. Inspect the links between the actual files

```shell
evidence=evidence/run-30187731233

# Confirm that the synthetic input produced the signed placeholder values.
openssl dgst -sha256 "${evidence}/dummy-snp-report.txt"
openssl dgst -sha384 "${evidence}/dummy-snp-report.txt"
jq '.snp' "${evidence}/rekor-v2-test-subject.json"

# Compare the signed subject digest with the digest recorded in the bundle.
openssl dgst -sha256 -binary \
  "${evidence}/rekor-v2-test-subject.json" | base64
jq -r '.messageSignature.messageDigest.digest' \
  "${evidence}/rekor-v2-test.sigstore.json"

# Inspect the selected v2 endpoint and the concrete Rekor entry.
jq '.rekorTlogUrls[] | select(.majorApiVersion == 2)' \
  "${evidence}/signing-config.json"
jq '.verificationMaterial.tlogEntries[0] | {
  kindVersion, logIndex, logId, inclusionProof
}' "${evidence}/rekor-v2-test.sigstore.json"

# Decode the exact body from which the Merkle leaf hash is calculated.
jq -r '.verificationMaterial.tlogEntries[0].canonicalizedBody' \
  "${evidence}/rekor-v2-test.sigstore.json" | base64 --decode | jq .
```

## 4. Run all verifications

Requirements are Cosign 3.1.2 or newer, OpenSSL, Go, and `jq`.

```shell
./scripts/verify_evidence.sh evidence/run-30187731233
```

The script performs three layers of verification:

1. It recomputes the dummy report's SHA-256 and SHA-384 and requires exact
   matches in a subject marked `placeholder` / `test-only`.
2. `cosign verify-blob` verifies the signed subject, Fulcio certificate chain,
   GitHub Actions OIDC identity, RFC 3161 timestamp, Rekor checkpoint signature,
   and transparency-log inclusion.
3. `verify_rekor_v2_bundle.go` independently implements the RFC 6962 Merkle
   calculation and verifies the C2SP checkpoint's Ed25519 signature using the
   matching Rekor v2 key from the TUF TrustedRoot.

## What the root-hash verification does

For the canonicalized Rekor entry `body`, the leaf hash is:

```text
SHA256(0x00 || body)
```

For each sibling in the audit path, an internal node is calculated as:

```text
SHA256(0x01 || left_child || right_child)
```

The result must equal both `inclusionProof.rootHash` and the root hash in the
signed C2SP checkpoint. The verifier also checks the tree size and verifies the
checkpoint's Ed25519 signature with the Rekor key selected by `logId` from the
TUF TrustedRoot.

Do not compare this historical root for equality with the log's latest root.
Every append changes the tree size and normally changes the root. Proving that a
later checkpoint extends an earlier one requires a separate consistency proof.

## Result for run 30187731233

| Field | Verified value |
| --- | --- |
| Rekor URL | `https://log2025-1.rekor.sigstore.dev` |
| Rekor API | v2 |
| Canonical entry | `hashedrekord` `0.0.2` |
| Log index | `40472493` |
| Checkpoint tree size | `40472501` |
| Audit-path hashes | `17` |
| Root hash, base64 | `beomKhVwk6AjkHPrfLivRynuYJQBZW4dnod6+vtVhlc=` |
| Log/checkpoint key ID, base64 | `zxGZFVvd0FEmjR8WrFwMdcAJ9vtaY/QXf44Y1wUeP6A=` |
| Subject digest, base64 | `F79C/73jpvhd28HLODuQckyKHru2BxZvnae/yZyD5hI=` |
| Placeholder report SHA-256 match | Valid |
| Placeholder measurement SHA-384 match | Valid (`test-only`, not SNP attestation verification) |
| GitHub workflow identity | `https://github.com/tkgsn/sigstore-public-good-instance-test/.github/workflows/rekor-v2.yml@refs/heads/main` |
| RFC 3161 timestamp | `2026-07-26 04:24:40 UTC` (`13:24:40 JST`) |
| Inclusion proof | Valid |
| Checkpoint signature | Valid |
| Cosign bundle verification | Valid |
