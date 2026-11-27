# セキュリティポリシー

[English](SECURITY.md) | [한국어](SECURITY.ko.md) | [简体中文](SECURITY.zh-CN.md) | 日本語

> 状態：プレリリース。以下は最初のリリースが約束するモデルです。*（予定）* の項目は未実装です。英語版と異なる場合は英語版を正とします。

## 脆弱性の報告

セキュリティの問題は公開の issue を**作らないでください**。

GitHub の非公開報告を使ってください：このリポジトリの **Security → Report a vulnerability**。バージョン、環境（ディストリビューション、Kubernetes のバージョン）、再現手順を含めてください。5 営業日以内の受領確認と、確認された問題への修正または緩和策の速やかな提供を目指します。希望する報告者は謝辞に記載します。

## サポート対象バージョン

1.0 までは最新のマイナーリリースのみがセキュリティ修正を受けます。1.0 以降は最新の二つのマイナーリリースです。

## セキュリティモデル

| 領域 | 約束 |
|------|------|
| 認証情報 | 既存の kubeconfig とその exec 認証プラグインを使います。認証情報やトークンを保存、複製、記録、送信しません。 |
| 読み取り専用が基本 | `status`、`why`、`logs`、`changes` は get/list/watch とログの読み取りだけを行います。 |
| 変更を伴う操作 | `sh`（pods/exec）、一時デバッグコンテナなどは明示的に実行し、出力に明記し、本番と判定した context では確認を求めます。 |
| 一括削除 | `rm` は必ず先にプレビューを表示し、確認後にだけ削除します。各 Pod は UID 指定で削除するため、同名で再作成された Pod には触れません。10 個以上は個数の入力が必要です。本番らしい context では context 名の入力（スクリプトでは `--yes --confirm-context=<name>`）が必要です。`--yes` を付けても `--max`（既定 50）が一回の上限になります。対象ごとの結果を報告します。 |
| kubeconfig への書き込み | `ctx` / `ns` は `current-context` と context の `namespace` フィールドだけを、client-go の標準ライター（`kubectl config use-context` と同じもの）で変更します。他の部分には触れません。記憶した「直前の値」は本人だけが読めるキャッシュファイル（モード 0600）に保存します。 |
| Secret | Secret の `data` は出力しません。Secret 由来の環境変数の値は伏せます。`changes` は ConfigMap や Secret のデータがいつ、どの field manager によって変わったかだけを報告します。値、キー名、ハッシュは出力しません。 |
| ログの伏せ字 | よくあるトークンやパスワードの形式は既定で伏せます。解除には `--no-redact` の明示が必要です。 |
| 更新 | `kubekaze update` は依頼されたときだけ実行し、バックグラウンドでは動きません。アーカイブがリリースの `checksums.txt` の SHA-256 と一致することを必須とし、置き換える前に新しいバイナリを一度実行し、一回のアトミックな rename で置き換えます（失敗時は古いバイナリが残ります）。https のみを受け付けます（平文 http は localhost のみ）。チェックサムはダウンロードが壊れていないことを示すだけで、誰がビルドしたかは示しません。特にオフライン網へ持ち込む前には、`checksums.txt` の cosign 署名（下記）をご自身で検証してください。 |
| テレメトリ | なし。対象の Kubernetes API サーバー以外への通信はしません（任意の AI 機能を有効にした場合のみ LLM エンドポイントに接続）。 |
| AI 機能*（予定）* | 既定でオフ。伏せ字処理済みでユーザーが事前確認（`--dry-run`）できるデータだけを、設定されたエンドポイントに送ります。ローカルモデルに対応します。 |
| デバッグイメージ | 一時デバッグコンテナは非特権です（権限昇格なし、全 capability を drop、既定の seccomp）。イメージは設定可能で、既定ではダイジェスト固定です。`latest` やタグなしのイメージは拒否します。 |
| 最小権限 | コマンドごとの必要な RBAC を文書化します。権限不足は回避せず、そのまま報告します。 |
| 端末の安全性 | クラスターが返す文字列（イベント、ログ、名前）の制御文字はエスケープし、端末エスケープシーケンスの注入を防ぎます。 |
| 複数クラスター*（予定）* | 一括実行は context ごとにその認証情報で行い、結果が認証情報をまたいで混ざることはありません。 |

## サプライチェーン

- リリースはタグ付きコミットから GitHub Actions でビルドし、Action はコミット SHA で固定します。
- 成果物には SHA-256 チェックサム、署名付きチェックサムファイル（cosign keyless）、SBOM（SPDX）、SLSA ビルド来歴が付きます。
- 依存関係は Dependabot で追跡し、`govulncheck` で検査します。
- 再現可能なビルド：`-trimpath`、静的バイナリ（`CGO_ENABLED=0`）、バージョン情報は ldflags で埋め込みます。

## リリースの検証

各リリースは `checksums.txt` と cosign keyless のバンドル `checksums.txt.sigstore.json` を公開します。使用するプラットフォームのアーカイブと一緒にダウンロードし（例は v0.1.0）、次を実行します：

```bash
# 1. アーカイブがチェックサム一覧と一致する
shasum -a 256 -c checksums.txt --ignore-missing      # Linux: sha256sum -c checksums.txt --ignore-missing

# 2. チェックサム一覧がこのリポジトリのリリースワークフローで署名されている
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/beomzh/kubekaze/\.github/workflows/release\.yml@refs/tags/v0\.1\.0$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

各アーカイブには SPDX SBOM（`*.sbom.json`）もあります。
