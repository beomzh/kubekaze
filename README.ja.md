# kubekaze

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
![Status](https://img.shields.io/badge/status-early%20development-orange)

[English](README.md) | [한국어](README.ko.md) | [简体中文](README.zh-CN.md) | 日本語

**何が問題で、なぜなのかをコマンド一つで教えてくれる、ターミナル中心の Kubernetes コンパニオン。**

> **状態：初期開発中（v0.2）。** 以下の主要コマンドは動作し、kind クラスターで試しています（一括削除は今のところ主に単体テストで確認）。0.x の間は粗い部分や変更があり得ます。[インストール](#インストール)を参照してください。設計の背景：[docs/CONCEPT.ja.md](docs/CONCEPT.ja.md)。

## なぜ作るのか

- k9s は閲覧には優れていますが、常駐型の UI であり、パイプにつないだりチケットに貼ったりするものではありません。
- stern はログを追えますが、何も説明しません。
- krew プラグインは小さく単機能で、結論は自分でつなぎ合わせる必要があります。
- 日々の小さな手間：このイメージは `bash` か `sh` か？ `logs -p` だったか？ いまどの context にいるのか？

## コマンド

| コマンド | 内容 | 状態 |
|----------|------|------|
| `kubekaze status` | 異常なものだけを表示：ノード（常に確認）、ワークロード、Pod、Job、PVC。問題があれば終了コード 3 | 利用可 |
| `kubekaze ls [kind [name]]` | namespace を一画面で概観（ワークロード、Job、Service、Ingress/Route、PVC、ConfigMap と Secret の名前。Secret の値は表示しない）。`ls deploy` などは一種類だけを一覧 | 利用可 |
| `kubekaze why <pod\|deploy/x\|node/x>` | Pod、ワークロード、ノードについて、タイムライン、根拠、可能性順の推定原因を表示 | 利用可 |
| `kubekaze events -A --type Warning` | namespace をまたいで時刻順に並べたイベント。`--summary` は namespace ごとの要約 | 利用可 |
| `kubekaze sh <pod\|deploy/x>` | 使えるシェル（bash/sh/ash）を自動で探す。なければ一時デバッグコンテナ | 利用可 |
| `kubekaze logs <pod\|deploy/x>` | 複数 Pod のログ追跡。再起動したコンテナは前回の実行ログも含む | 利用可 |
| `kubekaze top [ns]` | metrics-server による使用量スナップショット：ノードの割り当て可能量との比較（80% 超は赤）、`--sort cpu\|memory` の上位 Pod と limit に対する割合、`top ns` で namespace ごとの合計。`-w` でライブ表示 | 利用可 |
| `kubekaze rm pods --status CrashLoopBackOff,Evicted` | プレビューと確認つきの一括削除 | 利用可 |
| `kubekaze ctx [name\|-]` | context の切り替え。`oc` が namespace ごとに作る context をまとめ、ログイン期限切れをすぐに通知 | 利用可 |
| `kubekaze ns [name\|-]` | namespace（OpenShift ではプロジェクト）の切り替え。選択画面に Pod 数、異常な Pod 数、直近の警告を表示。スクリプトには `--list` / `-o json` | 利用可 |
| `kubekaze update` | バイナリの更新：オンライン、ミラー（`--url`）、ダウンロード済みアーカイブ（`--from`） | 利用可 |
| `kubekaze changes --since 1h` | 壊れる直前に何が変わったか：ロールアウト（イメージと設定の差分）、作成・編集されたオブジェクトと編集者、Helm のリビジョン、スケーリング、ノードイベント。`--since`、`-A` | 利用可 |
| `--all-contexts` / `--context 'prod-*'` | 複数クラスターに対して実行 | 予定 |
| AI 要約（オプトイン、ローカルモデル対応） | 平易な言葉での説明 | 予定 |

例：

```text
kubekaze status -A
kubekaze ls
kubekaze why deploy/api
kubekaze why node            # 全ノードを確認し、問題のあるものだけ表示
kubekaze why node/worker-3
kubekaze sh deploy/api -c sidecar
kubekaze sh api-7d9f -- ls -l /app
kubekaze logs deploy/api -f --since 10m
kubekaze events -A --summary
kubekaze changes --since 3h
kubekaze top -A --sort memory --limit 20   # metrics-server が必要
kubekaze top ns -w --interval 2s
kubekaze rm pods -A --status Evicted --dry-run
```

終了コード：`0` 正常、`1` エラー、`2` コマンド・フラグ・引数の誤り、`3` 問題を検出（`status`、`why`）。`sh` はリモートコマンドの終了コードをそのまま返します。

グローバルフラグ：`--kubeconfig`、`--context`、`-n/--namespace`、`-A/--all-namespaces`、`-o text|json`。

コマンドの出力と `--help` は、現時点では英語のみです。

## インストール

[Releases ページ](https://github.com/beomzh/kubekaze/releases)から OS と CPU に合うアーカイブ（Linux、macOS、Windows の amd64 または arm64）をダウンロードし、`checksums.txt` で確認してから、`kubekaze` バイナリを `PATH` に置いてください。

またはソースからビルドします（Go 1.26 以降）：

```bash
git clone https://github.com/beomzh/kubekaze.git
cd kubekaze
make build        # ./bin/kubekaze が作られます
```

リリース手順と検証：[docs/RELEASING.md](docs/RELEASING.md)（英語）。Homebrew と krew のパッケージは予定です。

## 更新

```bash
kubekaze update --check     # 新しいバージョンはあるか
kubekaze update             # ダウンロードし、チェックサムを確認して、バイナリを置き換える
```

kubekaze が自分で更新を確認することはありません。GitHub に接続するのは `update` を実行したときだけです。`/bin` や `/usr/local/bin` にインストールした場合は `sudo $(which kubekaze) update` を実行してください。

**閉域ネットワーク：**インターネットに接続できるマシンで `kubekaze_<version>_<os>_<arch>.tar.gz`（Windows は `.zip`）と `checksums.txt` をダウンロードし、両方をサーバーにコピーして `kubekaze update --from <アーカイブ>` を実行します。何もダウンロードせず、まず `checksums.txt` と照合します。**社内ミラー：**`kubekaze update --url https://mirror.example.com/kubekaze`（配置は [docs/RELEASING.md](docs/RELEASING.md)）。Homebrew や krew で管理しているインストールは `brew upgrade` / `kubectl krew upgrade` を使ってください。

v0.1.1 より前には `update` コマンドがありません。一度 v0.1.1 以降を手動でインストールしてから `update` を使ってください。

## シェル補完

```bash
kubekaze completion install      # $SHELL から bash、zsh、fish を判別し、ホームにファイルを書く
kubekaze completion bash         # またはスクリプトを出力：bash、zsh、fish、powershell
```

`completion install` が書き込むのはホームディレクトリ内のファイルだけです（他は変更しません）：

| シェル | ファイル | 備考 |
|--------|----------|------|
| bash | `~/.local/share/bash-completion/completions/kubekaze` | `bash-completion` パッケージが必要。初回使用時に読み込まれる |
| zsh | `~/.local/share/zsh/site-functions/_kubekaze` | ディレクトリが `fpath` に必要。`~/.zshrc` に追加する行はコマンドが表示する |
| fish | `~/.config/fish/completions/kubekaze.fish` | 自動で読み込まれる |
| PowerShell | なし | 表示された行を `$PROFILE` に追加 |

自分の起動ファイルを使いたい場合は、`install` を使わず `~/.bashrc`（または `~/.zshrc`）に `source <(kubekaze completion bash)` を書いてください。

`alias kk=kubekaze` を使っていますか？ シェルは補完をコマンド名に結び付けるため、エイリアスを kubekaze に伝える必要があります：`kubekaze completion install --alias kk`。`--alias` は補完をつなぐだけで `kk` コマンドは作りません。先に自分で定義してください（`~/.bashrc` に `alias kk=kubekaze`、またはシンボリックリンク）。これは `complete -F __start_kubekaze kk` と同じです。bash と fish は入力した名前で補完ファイルを読み込むため、エイリアス名の小さなファイル（`.../completions/kk`、`kk.fish`）も書き込みます。`~/.bashrc` に `source <(kubekaze completion bash --alias kk)` を書く方法もあります。

コマンド、フラグ、context、namespace、Pod、ワークロード（`deploy/...`）を、指しているクラスターからその場で取得して補完します。コマンドやフラグの打ち間違いには "Did you mean" の提案が出ます。どのコマンドも `--help` に詳しい説明と例があります（終了コードと環境変数は `kubekaze --help`）。

## 対応環境

標準の kubeconfig と client-go の認証を使うため、VM/kubeadm クラスター、EKS、AKS、GKE、OpenShift、Rancher 管理のクラスターで同じように使えます。複数クラスターへの一括実行（`--context 'prod-*'`）は予定です。

## 対応プラットフォーム

単体で完結するバイナリ一つ（`kubectl`、`oc`、シェルは不要）で、Linux、macOS、Windows の amd64 と arm64 に対応します。

## 原則

デフォルトは読み取り専用 · agent もサーバーもテレメトリもなし · 最小権限の RBAC · 秘密の値は出力しない · パイプと相性が良い（`-o json`）· ルール優先、AI は任意。

## セキュリティ

[SECURITY.ja.md](SECURITY.ja.md) を参照してください。脆弱性は GitHub の "Report a vulnerability" から非公開で報告してください。

## ドキュメント

- [コンセプト](docs/CONCEPT.ja.md)
- [リリースとパッケージング（英語）](docs/RELEASING.md)
- [セキュリティポリシー](SECURITY.ja.md)

## ライセンス

[Apache License 2.0](LICENSE) で提供されます。[NOTICE](NOTICE) も参照してください。

Kubernetes は The Linux Foundation の登録商標です。本プロジェクトは Kubernetes プロジェクト、Red Hat、AWS、Microsoft、Google、SUSE のいずれとも提携しておらず、承認も受けていません。
