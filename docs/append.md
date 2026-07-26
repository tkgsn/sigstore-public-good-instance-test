# Appendix: Why this test uses the Cosign CLI

この文書は、今回のRekor v2 Public Good Instanceテストで、署名処理を
すべて内包する単一のGitHub Actionではなく、公式Cosign CLIを直接実行した
理由を補足するものです。

## 結論

今回の目的は、単にファイルをkeyless署名することではなく、次の事項を
明示的に制御・観測することでした。

- Rekor v2を登録先として選択すること
- 実際に使用したSigningConfigを保存すること
- `hashedrekord 0.0.2`として登録されたことを確認すること
- Sigstore bundleを保存すること
- inclusion proof、checkpoint、Merkle root hashを個別に検証すること

このため、Rekor v2用SigningConfigを明示的に指定できる公式Cosign CLIを
使用しました。

## 使用している公式コンポーネント

今回のWorkflowは非公式な署名実装を使用しているわけではありません。

| 処理 | 使用しているもの |
| --- | --- |
| リポジトリのcheckout | `actions/checkout@v4` |
| Cosignのインストール | `sigstore/cosign-installer@v4.1.2` |
| keyless署名とRekor登録 | 公式Cosign `v3.1.2` CLI |
| bundle検証 | 公式Cosign `verify-blob` |
| 証跡Artifactの保存 | `actions/upload-artifact@v4` |

実装は
[`rekor-v2.yml`](../.github/workflows/rekor-v2.yml)を参照してください。

## 単一Actionで通常できること

Sigstore公式の
[`sigstore/gh-action-sigstore-python`](https://github.com/sigstore/gh-action-sigstore-python)
を使うと、次の処理を一つのstepにまとめられます。

- GitHub Actions OIDCを使ったkeyless署名
- 署名対象ファイルの指定
- 署名後のidentity検証
- 署名生成物のGitHub Actions Artifactへの保存
- productionまたはstaging環境の選択

通常のCIやRelease成果物を標準設定で署名する場合は、このActionを使う方が
Workflowを短くできます。

## 今回、単一Actionを使わなかった理由

2026年7月26日に確認した`gh-action-sigstore-python`の公開inputには、任意の
SigningConfigを渡す設定や、Rekor v2専用TUF targetを選択する設定がありません。
`staging: true`はstaging環境への切り替えであり、production環境のRekor v2を
選択する設定ではありません。

一方、今回使用したCosignでは、次のコマンドでTUFからRekor v2用の設定を
明示的に取得できます。

```shell
cosign signing-config create \
  --with-default-rekor-v2 \
  --out signing-config.json
```

このコマンドは、通常の`signing_config.v0.2.json`ではなく、Rekor v2用の
`signing_config_rekor_v2.v0.2.json`をTUFから取得します。今回保存した設定は
[`signing-config.json`](../evidence/run-30187731233/signing-config.json)で確認できます。

取得した設定を署名時に明示します。

```shell
cosign sign-blob \
  --yes \
  --signing-config signing-config.json \
  --bundle rekor-v2-test.sigstore.json \
  rekor-v2-test-subject.json
```

これにより、どのSigningConfigを使い、どのRekor API versionを選択したかを
証跡として残せます。

## 通常設定とRekor v2用設定の違い

検証時点では、通常のTUF SigningConfigが示すRekorの既定登録先はRekor v1の
`https://rekor.sigstore.dev`です。Rekor v2用SigningConfigには、より高いAPI
versionとして次のサービスが含まれていました。

```json
{
  "url": "https://log2025-1.rekor.sigstore.dev",
  "majorApiVersion": 2,
  "operator": "sigstore.dev"
}
```

Rekor v2のlog URLはshardingによって変わるため、WorkflowへURLを直接
hardcodeせず、`--with-default-rekor-v2`でTUF配布設定を取得しています。

## このテストでCLIが提供した観測可能性

単一Actionに処理をまとめず、各処理を明示したことで、以下を個別の証跡として
保存できました。

| 証跡 | 保存先 | 確認できること |
| --- | --- | --- |
| 署名対象 | [`rekor-v2-test-subject.json`](../evidence/run-30187731233/rekor-v2-test-subject.json) | 署名したcommit、workflow、run ID、テスト用SNPフィールド |
| 仮report | [`dummy-snp-report.txt`](../evidence/run-30187731233/dummy-snp-report.txt) | 仮measurementの導出元。実際のSNP reportではない |
| Sigstore bundle | [`rekor-v2-test.sigstore.json`](../evidence/run-30187731233/rekor-v2-test.sigstore.json) | 証明書、署名、timestamp、Rekor entry、inclusion proof、checkpoint |
| SigningConfig | [`signing-config.json`](../evidence/run-30187731233/signing-config.json) | Rekor v2 endpointとAPI versionの選択 |

これらの対応関係と検証方法は
[`verification.md`](verification.md)に記載しています。

## 仮SNP measurementを追加した今回のsubject

run `30187731233`では、データの流れを確認するため、Workflowが
`dummy-snp-report.txt`のSHA-256とSHA-384を計算し、署名前のsubjectへ次の形で
格納しています。

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
      "result": "test-only"
    }
  }
}
```

このJSON全体のSHA-256 digestが署名され、Rekor entryへ入ります。Rekorが
`snp.measurement.value`を個別フィールドとして解釈・検証するわけではありません。
検証者はsubject、bundle、仮reportを一緒に取得し、まずbundleに対するsubjectの
署名を検証し、次に仮reportから計算した値がsubject内の値と一致することを確認します。

ここでの値はAMD SEV-SNP attestationから取得したmeasurementではありません。
`mode: placeholder`と`result: test-only`はその区別を明示するためのものです。

## 使い分け

| 要件 | 推奨方法 |
| --- | --- |
| 標準設定で成果物を簡単にkeyless署名したい | `gh-action-sigstore-python` |
| GitHub Release成果物を簡潔に署名したい | `gh-action-sigstore-python` |
| Rekor v2を明示的に選択したい | Cosign CLIとRekor v2 SigningConfig |
| 使用したSigningConfig自体を保存したい | Cosign CLI |
| inclusion proofやroot hashを研究・検証したい | Cosign CLIでbundleを保存し、独立検証を追加 |

将来、通常のTUF SigningConfigがRekor v2を既定として配布するようになった場合や、
単一ActionがRekor v2用SigningConfigを公開inputとして受け取れるようになった場合は、
単一Actionへ簡略化できる可能性があります。

## Action versionの固定について

このリポジトリは検証用のため、Actionをrelease tagで指定しています。本番環境では
supply-chainリスクを抑えるため、`actions/checkout`、`cosign-installer`、
`upload-artifact`を完全なcommit SHAで固定することを推奨します。
