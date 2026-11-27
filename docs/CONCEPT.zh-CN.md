# 产品理念

> **kubekaze**（風，日语“风”之意）。[English](CONCEPT.md) | [한국어](CONCEPT.ko.md) | 简体中文 | [日本語](CONCEPT.ja.md)

## 一句话介绍

一款以终端为中心的 Kubernetes 辅助工具：一条命令回答**“哪里出了问题，为什么？”**，并消除 `exec`、`logs -p` 和切换 context 带来的日常麻烦。

## 定位

| 工具 | 擅长 | 本项目填补的空缺 |
|------|------|------------------|
| k9s | 交互式浏览（TUI） | 不是可以接管道、写脚本、贴进工单的命令 |
| stern | 多 Pod 日志跟踪 | 只有日志，不分析原因 |
| krew 插件 | 众多单一用途的小工具 | 结果仍需手工拼接 |
| kubectl | 什么都能做，原始输出 | 得出一个结论需要很多命令和选项 |

设计立场：**输入命令，得到答案。**没有常驻界面。输出默认面向人阅读，加 `-o json` 则面向机器。

## 支柱

### 1. 排障（故障响应）

- `status`：只显示命名空间或集群中不健康的对象（CrashLoopBackOff、Pending、ImagePullBackOff、失败的 Job、NotReady 节点、快满的 PVC、即将过期的证书）。健康对象不显示。
- `ls [kind]`：一屏概览命名空间中的内容（带就绪数/期望数的工作负载、Job、Service、Ingress/Route、PVC、ConfigMap 和 Secret 名称），异常项在前。Secret 只列名称，从不显示值。
- `why <kind>/<name>`：为一个对象生成关联时间线：当前状态、事件、上次终止原因（OOMKilled、退出码）、前一个容器日志的末尾、属主链（Pod → ReplicaSet → Deployment）以及最近的变更。最后给出由确定性规则得出、按可能性排序的**疑似原因**。
- `events`：按实际需要的方式排序事件，这是 `kubectl get events` 默认做不到的。
  - `events -A`（所有命名空间）或 `events -n a,b`（多个）；默认按最后出现时间排序，最新的在最下面。
  - `--sort time|count|type|namespace|object`、`--type Warning`、`--since 30m`、`--reason BackOff,FailedScheduling`、`--for pod/api`。
  - 重复事件合并并显示次数（`×37 over 12m`），避免刷屏；`--no-collapse` 显示原始事件。
  - `--summary` 每个命名空间一行（告警数、主要原因），快速看出哪个命名空间在“起火”；`-w` 实时跟踪新事件。
  - 注意：Kubernetes 默认只保留约一小时的事件；结果可能不完整时工具会说明。
- `changes --since 1h`：“故障发生前刚刚改了什么？”（发布、新建和修改过的对象及修改它的 field manager、Helm 版本、扩缩容、节点事件）。ConfigMap 和 Secret 的修改只报告为“数据已变更”。API 不保留已删除的对象和旧值，因此不显示。

### 2. 日常麻烦

- `sh <pod>`：不用再猜镜像里是 bash 还是 sh。
  1. 解析目标：Pod 名、`deploy/x` 或标签选择器 → 选一个 Running 的 Pod；容器按 `kubectl.kubernetes.io/default-container` 注解选择，否则询问。
  2. 用非交互 exec 依次探测候选 shell（`bash`、`sh`、`ash`、`busybox sh`），打开第一个可用的。
  3. 如果镜像没有 shell（distroless/scratch），提供**临时调试容器**（`kubectl debug` 语义），镜像可配置且固定版本。
  4. 在 OpenShift 集群上行为与 `oc rsh` 一致（相同默认值），但不需要 `oc`。
- `logs`：stern 风格的多 Pod 日志跟踪，并能感知崩溃：容器重启过时自动带上前一个容器的日志（即 `-p` 的作用），并高亮错误。
- `top`：基于 metrics API（metrics-server）的只读用量快照：节点用量对比可分配量、按 CPU 或内存排序的高用量 Pod（用量以占 limit 的百分比显示，便于发现限流或即将 OOM 的 Pod）、按命名空间汇总（`top ns`）和 `-w` 实时视图。没有 metrics-server 时会说明需要安装什么，而不是抛出一堆错误。
- `rm`：批量删除 Pod（及其他资源），无需拼接 `get | grep | awk | xargs kubectl delete`。
  - 按名称模式、标签、状态（`--status CrashLoopBackOff,Error,Evicted,Completed`）、存在时间（`--older-than 2h`）、节点筛选，或从列表中交互选择。
  - **总是先预览**（精确列出将删除对象的 dry run），确认后才删除。删除大量对象，或在标记为生产的 context 上执行任何删除时，需要输入数量或名称；`--yes` 供脚本使用，仍受安全上限约束。
  - 逐个对象报告结果，绝不静默跳过失败。
- `ctx` 和 `ns`：在一个工具里切换集群 context 和命名空间，类似 kubectx/kubens。
  - `ctx` / `ns` 打开模糊匹配列表；`ctx prod-eks`、`ns payments`、`ns -`（上一个）可直接使用。
  - **OpenShift 项目：**在 OpenShift 上，`ns` 用同一个命令列出并切换项目（等同于 `oc projects` / `oc project`）。
  - 类似生产环境的 context 用颜色标记，变更类命令在其上要求确认。
  - 变更类命令的提示行总是显示当前 context 和命名空间，让你在操作前知道自己在哪里。

### 3. 多集群、任意发行版

- 使用标准 kubeconfig 和 client-go 认证，因此 EKS（`aws eks get-token`）、AKS（kubelogin）、GKE（gke-gcloud-auth-plugin）、Rancher（代理 kubeconfig）、kubeadm/虚拟机集群和 OpenShift 走同一条路径。本工具从不自行实现云厂商认证。
- 并行执行：`--context 'prod-*'` 或 `--all-contexts` 跨集群并行运行 `status` / `why`，并按集群合并结果。
- 针对发行版的提示（托管节点组事件、OpenShift SCC 拒绝、Rancher 项目/命名空间特性）以可选、可检测的规则包形式提供。

## 原则

1. **默认只读。**任何改变状态的操作（`sh`、调试容器、重启）都需显式发起并清楚标明。
2. **无 agent、无服务端、无遥测。**一个静态二进制，使用你现有的 kubeconfig。
3. **最小权限。**记录每个命令所需的最小 RBAC；缺少权限时优雅降级（并说明）。
4. **从不打印机密值。**Secret 数据和来自 Secret 的环境变量值默认脱敏。
5. **便于接管道。**稳定的退出码和 `-o json`，可用于脚本和 CI。
6. **规则优先，AI 可选。**

## 关于“AI 摘要”

这是一个可选且**默认关闭**的功能：把已经收集并脱敏的诊断信息（事件、状态、日志末尾）发送给你配置的 LLM（托管 API 或 Ollama 等本地模型），生成自然语言的解释和建议的下一步。由于日志和事件可能含敏感数据，必须显式开启、必须支持仅本地模型，并且必须能原样展示将发送的内容（`--dry-run`）。MVP 只提供规则；AI 是之后可插拔的一层。

## MVP 范围

`status`、`ls`、`why`、`events`、`sh`、`logs`、`top`、`rm`、`ctx`、`ns` 和 `changes`。其余功能（并行执行、规则包、AI）在后续次版本中加入。

## 支持平台

单个自包含二进制，没有运行时依赖（不需要 `kubectl`、`oc` 或 shell）。支持目标：

| OS | amd64 | arm64 |
|----|:-----:|:-----:|
| Linux | 支持 | 支持 |
| macOS | 支持 | 支持（Apple Silicon） |
| Windows | 支持 | 支持 |

Windows 是一等支持目标：交互式 `sh` 使用 Windows 控制台 API 进入原始终端模式，路径和 `KUBECONFIG` 分隔符遵循操作系统规则，颜色可平稳降级。同一 CI 矩阵在三种操作系统上运行测试。

## 技术

- Go、client-go、单一静态二进制；以独立 CLI 分发，也通过 krew 作为 `kubectl-` 插件分发。
- 打包和版本策略见 [RELEASING.md](RELEASING.md)，安全模型见 [../SECURITY.zh-CN.md](../SECURITY.zh-CN.md)。

## 未决问题

- 最终名称和 CLI 命令。
- 规则格式：内置于二进制，还是从第一天起就支持可加载的 YAML 规则包？
- 在没有集群内组件的情况下，`changes` 能做到什么程度（Kubernetes 默认只保留约一小时的事件）。
