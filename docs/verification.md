# Rekor v2 evidence verification

This document shows how to download a GitHub Actions artifact and verify its
Sigstore bundle, Rekor v2 inclusion proof, checkpoint signature, and Merkle root.

## 1. Sign in to GitHub and download the artifact

### GitHub CLI

Install the [GitHub CLI](https://cli.github.com/), then sign in:

```shell
gh auth login --hostname github.com --git-protocol ssh --web
gh auth status --hostname github.com
```

Download the evidence from run `30183326061`:

```shell
./scripts/download_evidence.sh 30183326061 evidence-30183326061
```

The equivalent direct command is:

```shell
gh run download 30183326061 \
  --repo tkgsn/sigstore-public-good-instance-test \
  --name rekor-v2-evidence-30183326061-1 \
  --dir evidence-30183326061
```

### GitHub web UI

1. Sign in to GitHub.
2. Open [Actions run 30183326061](https://github.com/tkgsn/sigstore-public-good-instance-test/actions/runs/30183326061).
3. In the **Artifacts** section, select `rekor-v2-evidence-30183326061-1`.
4. Extract the downloaded ZIP file.

GitHub requires authentication even when downloading Actions artifacts from a
public repository.

The files downloaded from this run are also committed at
[`evidence/run-30183326061`](../evidence/run-30183326061). They are public
verification material and contain no GitHub credential or private signing key.

## 2. Downloaded files and how they are related

The committed files are byte-for-byte copies of the three files extracted from
the GitHub Actions artifact `rekor-v2-evidence-30183326061-1`:

| Downloaded file | Role in this test |
| --- | --- |
| [`rekor-v2-test-subject.json`](../evidence/run-30183326061/rekor-v2-test-subject.json) | The artifact that was actually signed. It identifies commit `3069fcd0c32873ef580c02e20e955cdb776b1f4a`, workflow run `30183326061`, and run attempt `1`. |
| [`rekor-v2-test.sigstore.json`](../evidence/run-30183326061/rekor-v2-test.sigstore.json) | The Sigstore bundle returned by signing. It contains the Fulcio certificate, artifact signature and digest, RFC 3161 timestamp, canonicalized Rekor v2 entry, Merkle inclusion proof, root hash, and signed checkpoint. This is the main verification evidence. |
| [`signing-config.json`](../evidence/run-30183326061/signing-config.json) | The TUF-distributed service configuration used by Cosign when signing. Its `rekorTlogUrls` contains `https://log2025-1.rekor.sigstore.dev` with `majorApiVersion: 2`. It records service selection, but is not itself cryptographic proof of inclusion. |

The relationship between the files is:

```text
rekor-v2-test-subject.json
  -> SHA-256 digest and keyless signature
  -> canonicalized hashedrekord 0.0.2 entry in rekor-v2-test.sigstore.json
  -> RFC 6962 leaf hash and audit path
  -> root hash in the signed Rekor checkpoint

signing-config.json
  -> tells Cosign to submit that entry to the Rekor v2 Public Good Instance
```

For this downloaded subject, the base64-encoded SHA-256 digest is:

```text
nQWxW5dcj0y4KatMK/Jy1rCfntcv3f2IZtCMeJcIc9U=
```

It is stored at `messageSignature.messageDigest.digest` in the downloaded
bundle. The same signature and certificate are embedded in the bundle's
base64-encoded `canonicalizedBody`, which is the exact Rekor leaf payload.

The following commands inspect the links between the actual files:

```shell
evidence=evidence/run-30183326061

# The signed subject and the digest recorded in its bundle.
jq . "${evidence}/rekor-v2-test-subject.json"
openssl dgst -sha256 -binary \
  "${evidence}/rekor-v2-test-subject.json" | base64
jq -r '.messageSignature.messageDigest.digest' \
  "${evidence}/rekor-v2-test.sigstore.json"

# The selected v2 log endpoint.
jq '.rekorTlogUrls[] | select(.majorApiVersion == 2)' \
  "${evidence}/signing-config.json"

# The concrete Rekor entry type and its inclusion evidence.
jq '.verificationMaterial.tlogEntries[0] | {
  kindVersion,
  logIndex,
  logId,
  inclusionProof
}' "${evidence}/rekor-v2-test.sigstore.json"

# Decode the exact canonicalized body used to calculate the Merkle leaf hash.
jq -r '.verificationMaterial.tlogEntries[0].canonicalizedBody' \
  "${evidence}/rekor-v2-test.sigstore.json" | base64 --decode | jq .
```

## 3. Run all verifications

Requirements are Cosign 3.1.2 or newer, Go, and `jq`.

```shell
./scripts/verify_evidence.sh evidence-30183326061
```

To verify the copy preserved in this repository instead, run:

```shell
./scripts/verify_evidence.sh evidence/run-30183326061
```

The script obtains the Sigstore TrustedRoot through TUF, then performs two
layers of verification:

1. `cosign verify-blob` verifies the signed artifact, Fulcio certificate chain,
   GitHub Actions OIDC identity, RFC 3161 timestamp, Rekor checkpoint signature,
   and transparency-log inclusion.
2. `verify_rekor_v2_bundle.go` independently implements the RFC 6962 Merkle
   calculation and verifies the C2SP checkpoint's Ed25519 signature with the
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

The resulting root must equal both:

- `inclusionProof.rootHash`; and
- the root hash in the signed C2SP checkpoint.

The verifier also checks that the checkpoint tree size equals
`inclusionProof.treeSize`, then verifies the checkpoint's Ed25519 signature
using the Rekor key selected by `logId` from the TUF TrustedRoot.

Do not compare this historical root for equality with the log's latest root.
Every appended entry changes the tree size and usually changes the root. A
bundle proves inclusion at its signed historical checkpoint. Establishing that
a later checkpoint extends an earlier checkpoint is a separate consistency
proof, computed from Rekor v2 tiles.

## Result for run 30183326061

The downloaded bundle produced the following verified values:

| Field | Value |
| --- | --- |
| Rekor URL | `https://log2025-1.rekor.sigstore.dev` |
| Rekor API | v2 |
| Canonical entry | `hashedrekord` `0.0.2` |
| Log index | `40335848` |
| Checkpoint tree size | `40335875` |
| Audit-path hashes | `20` |
| Root hash, base64 | `mBopqZh02Z4IzwgZbB7RKYvBUh/PLWgpxWS159pAS48=` |
| Log/checkpoint key ID, base64 | `zxGZFVvd0FEmjR8WrFwMdcAJ9vtaY/QXf44Y1wUeP6A=` |
| GitHub workflow identity | `https://github.com/tkgsn/sigstore-public-good-instance-test/.github/workflows/rekor-v2.yml@refs/heads/main` |
| Inclusion proof | Valid |
| Checkpoint signature | Valid |
| Cosign bundle verification | Valid |

At the time of the follow-up check, the live checkpoint had already advanced to
tree size `40338937`, so its root differed as expected.
