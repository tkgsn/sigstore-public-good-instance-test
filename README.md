# Sigstore Public Good Instance test

This repository contains a manually triggered GitHub Actions workflow that
performs a keyless signature with Cosign and uploads the transparency-log entry
to the Rekor v2 Public Good Instance.

## Run the test

1. Open **Actions** and select **Rekor v2 public-good test**.
2. Select **Run workflow**.
3. After the job succeeds, download the `rekor-v2-evidence-*` artifact.

The artifact contains:

- `rekor-v2-test-subject.json`: the signed test subject.
- `rekor-v2-test.sigstore.json`: the Sigstore bundle, including the Rekor
  transparency-log evidence.
- `signing-config.json`: the TUF-distributed service configuration used for
  signing.

The workflow obtains its Rekor v2 endpoint from Sigstore's TUF repository by
running:

```shell
cosign signing-config create \
  --with-default-rekor-v2 \
  --out signing-config.json
```

It intentionally does not hardcode a Rekor shard URL because the Public Good
Instance is periodically sharded.

## Important

The workflow is manual only. Running it creates a permanent public transparency
log entry. The repository name, workflow identity, artifact digest, certificate,
and timestamp become public; the artifact contents themselves are not uploaded
to Rekor.

## Verify downloaded evidence

See [docs/verification.md](docs/verification.md) for GitHub authentication and
artifact-download instructions, independent inclusion-proof/root-hash
verification, and the verified results from run `30183326061`.

The evidence downloaded from that run is also preserved under
[`evidence/run-30183326061`](evidence/run-30183326061), so it remains verifiable
after the GitHub Actions artifact expires.
