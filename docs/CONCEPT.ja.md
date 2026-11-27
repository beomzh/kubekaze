# コンセプト

> **kubekaze**（風）。[English](CONCEPT.md) | [한국어](CONCEPT.ko.md) | [简体中文](CONCEPT.zh-CN.md) | 日本語

## 一行で言うと

**「何が問題で、なぜなのか」**にコマンド一つで答え、`exec`、`logs -p`、context の切り替えといった日々の手間をなくす、ターミナル中心の Kubernetes コンパニオン。

## 位置づけ

| ツール | 得意なこと | このプロジェクトが埋める隙間 |
|--------|------------|------------------------------|
| k9s | 対話的な閲覧（TUI） | パイプやスクリプト、チケット貼り付けに使えるコマンドではない |
| stern | 複数 Pod のログ追跡 | ログだけで、原因分析はない |
| krew プラグイン | 多数の小さな単機能ツール | 結果は手作業でつなぎ合わせる必要がある |
| kubectl | 何でもできる、生の出力 | 一つの結論に至るまで多くのコマンドとフラグが要る |

設計の姿勢：**コマンドを入れると答えが出る。**常駐 UI はありません。出力は既定で人が読むための形式で、`-o json` で機械可読になります。

## 柱

### 1. トリアージ（障害対応）

- `status`：namespace やクラスターの異常なものだけを表示します（CrashLoopBackOff、Pending、ImagePullBackOff、失敗した Job、NotReady のノード、満杯に近い PVC、期限が近い証明書）。正常なものは隠します。
- `ls [kind]`：namespace にあるものを一画面で概観します（ready/desired 付きのワークロード、Job、Service、Ingress/Route、PVC、ConfigMap と Secret の名前）。異常なものが先頭です。Secret は名前だけを表示し、値は出しません。
- `why <kind>/<name>`：一つのオブジェクトの関連タイムライン：現在の状態、イベント、直近の終了理由（OOMKilled、終了コード）、前のコンテナのログ末尾、オーナーチェーン（Pod → ReplicaSet → Deployment）、最近の変更。最後に決定的なルールが出した**推定原因**を可能性順に示します。
- `events`：実際に必要な順序でイベントを並べます。`kubectl get events` が既定ではしないことです。
  - `events -A`（全 namespace）や `events -n a,b`（複数）。既定は最終観測時刻順で、新しいものほど下に出ます。
  - `--sort time|count|type|namespace|object`、`--type Warning`、`--since 30m`、`--reason BackOff,FailedScheduling`、`--for pod/api`。
  - 繰り返しのイベントは回数（`×37 over 12m`）にまとめ、画面を埋め尽くしません。`--no-collapse` で生のイベントを表示します。
  - `--summary` は namespace ごとに一行（警告数と主な理由）で、どの namespace が燃えているかをすぐに把握できます。`-w` は新しいイベントを追跡します。
  - 注：Kubernetes がイベントを保持するのは既定で約一時間です。結果が不完全な可能性があるときはツールがそう伝えます。
- `changes --since 1h`：「壊れる直前に何が変わったか」（ロールアウト、ハッシュのみで示す ConfigMap/Secret のリビジョン、スケーリング、ノードイベント）。

### 2. 日々の手間

- `sh <pod>`：イメージに bash があるのか sh なのかを推測する必要はありません。
  1. 対象の解決：Pod 名、`deploy/x`、ラベルセレクター → Running の Pod を選びます。コンテナは `kubectl.kubernetes.io/default-container` アノテーションで選び、なければ尋ねます。
  2. 非対話 exec で候補のシェル（`bash`、`sh`、`ash`、`busybox sh`）を順に試し、最初に動いたものを開きます。
  3. シェルがないイメージ（distroless/scratch）では、**一時デバッグコンテナ**（`kubectl debug` 相当）を提案します。イメージは設定可能で固定されています。
  4. OpenShift では `oc` を必要とせずに `oc rsh` と同じ既定動作をします。
- `logs`：stern 風の複数 Pod 追跡に加えてクラッシュを認識します。コンテナが再起動していれば前のコンテナのログを自動で含め（`-p` の役割）、エラーを強調します。
- `top`：metrics API（metrics-server）の読み取り専用スナップショット：ノードの割り当て可能量との比較、CPU やメモリの上位 Pod（limit に対する割合で表示し、スロットリングや OOM 寸前の Pod が目立つ）、namespace ごとの合計（`top ns`）、`-w` のライブ表示。metrics-server がなければ、エラーを並べる代わりに何を入れるべきかを伝えます。
- `rm`：`get | grep | awk | xargs kubectl delete` をつなげずに Pod（などのリソース）を一括削除します。
  - 名前パターン、ラベル、状態（`--status CrashLoopBackOff,Error,Evicted,Completed`）、経過時間（`--older-than 2h`）、ノードで選ぶか、一覧から対話的に選びます。
  - **必ず先にプレビュー**（削除対象を正確に列挙する dry run）し、確認後にだけ削除します。大量の削除や、本番とされた context での操作には個数や名前の入力が必要です。`--yes` はスクリプト用で、安全上限は守られます。
  - 対象ごとの結果を報告し、失敗を黙って飛ばすことはありません。
- `ctx` と `ns`：kubectx/kubens のように、context と namespace の切り替えを一つのツールで行います。
  - `ctx` / `ns` はあいまい検索の一覧を開きます。`ctx prod-eks`、`ns payments`、`ns -`（直前）は直接実行できます。
  - **OpenShift プロジェクト：**OpenShift では同じ `ns` コマンドでプロジェクトを一覧・切り替えします（`oc projects` / `oc project` 相当）。
  - 本番らしい context は色で示し、変更を伴うコマンドはそこで確認を求めます。
  - 変更を伴うコマンドのプロンプト行には現在の context と namespace を常に表示し、実行前に自分の位置がわかります。

### 3. 複数クラスター、どのディストリビューションでも

- 標準の kubeconfig と client-go の認証を使うため、EKS（`aws eks get-token`）、AKS（kubelogin）、GKE（gke-gcloud-auth-plugin）、Rancher（プロキシ kubeconfig）、kubeadm/VM クラスター、OpenShift が同じ経路で動きます。クラウド認証を独自に実装することはありません。
- 一括実行：`--context 'prod-*'` や `--all-contexts` で `status` / `why` をクラスター横断で並列実行し、クラスターごとに結果をまとめます。
- ディストリビューション固有のヒント（マネージドノードグループのイベント、OpenShift の SCC 拒否、Rancher の project/namespace の癖）は、任意で検出可能なルールパックとして追加します。

## 原則

1. **デフォルトは読み取り専用。**状態を変える操作（`sh`、デバッグコンテナ、再起動）は明示的で、はっきり表示します。
2. **agent なし、サーバーなし、テレメトリなし。**既存の kubeconfig を使う静的バイナリ一つ。
3. **最小権限。**コマンドごとの最小 RBAC を文書化し、権限がなければ穏やかに縮退します（そのことも伝えます）。
4. **秘密の値は出力しない。**Secret のデータと Secret 由来の環境変数は既定で伏せます。
5. **パイプと相性が良い。**安定した終了コードと `-o json` でスクリプトや CI で使えます。
6. **ルールが先、AI は任意。**

## 「AI 要約」について

任意で**既定はオフ**の機能です。収集して伏せ字にした診断情報（イベント、状態、ログ末尾）を、設定した LLM（ホスト型 API または Ollama などのローカルモデル）に送り、平易な説明と次の手順の提案を作ります。ログやイベントには機密データが含まれ得るため、明示的な有効化が必須で、ローカル専用モデルに対応し、送信内容をそのまま表示できる（`--dry-run`）必要があります。MVP はルールのみで、AI は後から差し込める層です。

## MVP の範囲

`status`、`ls`、`why`、`events`、`sh`、`logs`、`top`、`rm`、`ctx`、`ns`。それ以外（`changes`、一括実行、ルールパック、AI）は後のマイナーリリースで追加します。

## プラットフォーム

ランタイム依存のない単体バイナリです（`kubectl`、`oc`、シェルは不要）。対応対象：

| OS | amd64 | arm64 |
|----|:-----:|:-----:|
| Linux | 対応 | 対応 |
| macOS | 対応 | 対応（Apple Silicon） |
| Windows | 対応 | 対応 |

Windows も第一級の対象です。対話的な `sh` は Windows コンソール API で raw モードを使い、パスと `KUBECONFIG` の区切りは OS の規則に従い、色は無理なく縮退します。同じ CI マトリクスで三つの OS すべてのテストを実行します。

## 技術

- Go、client-go、静的バイナリ一つ。単体の CLI として、また krew 経由の `kubectl-` プラグインとして配布します。
- パッケージングとバージョニングは [RELEASING.md](RELEASING.md)、セキュリティモデルは [../SECURITY.ja.md](../SECURITY.ja.md) を参照してください。

## 未決の課題

- 最終的な名前と CLI コマンド。
- ルールの形式：バイナリに組み込むか、最初から読み込み可能な YAML ルールパックにするか。
- クラスター内コンポーネントなしで `changes` をどこまで実現するか（Kubernetes がイベントを保持するのは既定で約一時間）。
