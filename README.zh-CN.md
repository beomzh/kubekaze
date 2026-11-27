# kubekaze

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
![Status](https://img.shields.io/badge/status-early%20development-orange)

[English](README.md) | [한국어](README.ko.md) | 简体中文 | [日本語](README.ja.md)

**一款以终端为中心的 Kubernetes 辅助工具：一条命令告诉你哪里出了问题、为什么。**

> **状态：早期开发阶段（v0.2）。** 下列核心命令可以使用，并在 kind 集群上试过（批量删除目前主要由单元测试覆盖）。0.x 版本之间可能存在不完善之处和变更。参见[安装](#安装)。设计背景：[docs/CONCEPT.zh-CN.md](docs/CONCEPT.zh-CN.md)。

## 为什么做这个工具

- k9s 很适合浏览，但它是常驻界面，无法接管道，也不方便把结果贴进工单。
- stern 能跟踪日志，但不做任何解释。
- krew 插件小而单一，结论还是要自己拼起来。
- 日常的小麻烦：这个镜像里是 `bash` 还是 `sh`？刚才是不是该用 `logs -p`？我现在在哪个 context？

## 命令

| 命令 | 作用 | 状态 |
|------|------|------|
| `kubekaze status` | 只显示不健康的对象：节点（总是检查）、工作负载、Pod、Job、PVC。发现问题时退出码为 3 | 可用 |
| `kubekaze ls [kind [name]]` | 一屏概览命名空间（工作负载、Job、Service、Ingress/Route、PVC、ConfigMap 和 Secret 名称；从不显示 Secret 的值）；`ls deploy` 等只列出一种资源 | 可用 |
| `kubekaze why <pod\|deploy/x\|node/x>` | 针对 Pod、工作负载或节点给出时间线、证据和按可能性排序的疑似原因 | 可用 |
| `kubekaze events -A --type Warning` | 跨命名空间按时间排序的事件；`--summary` 按命名空间汇总 | 可用 |
| `kubekaze sh <pod\|deploy/x>` | 自动找到可用的 shell（bash/sh/ash），或使用临时调试容器 | 可用 |
| `kubekaze logs <pod\|deploy/x>` | 同时跟踪多个 Pod 的日志；重启过的容器会带上前一次运行的日志 | 可用 |
| `kubekaze top [ns]` | 基于 metrics-server 的用量快照：节点用量对比可分配量（超过 80% 标红）、按 `--sort cpu\|memory` 排序的高用量 Pod 及其占 limit 的百分比、`top ns` 按命名空间汇总；`-w` 实时刷新 | 可用 |
| `kubekaze rm pods --status CrashLoopBackOff,Evicted` | 批量删除，先预览再确认 | 可用 |
| `kubekaze ctx [name\|-]` | 切换 context；将 `oc` 按命名空间生成的 context 归组；登录过期时立即提醒 | 可用 |
| `kubekaze ns [name\|-]` | 切换命名空间（OpenShift 上为项目）；选择器显示每个命名空间的 Pod 数、异常 Pod 数和近期告警，脚本可用 `--list` / `-o json` | 可用 |
| `kubekaze update` | 更新二进制：在线、从镜像站（`--url`）或从已下载的压缩包（`--from`） | 可用 |
| `kubekaze changes --since 1h` | 故障发生前刚刚改了什么 | 计划中 |
| `--all-contexts` / `--context 'prod-*'` | 跨多个集群执行 | 计划中 |
| AI 摘要（需手动开启，支持本地模型） | 用自然语言解释 | 计划中 |

示例：

```text
kubekaze status -A
kubekaze ls
kubekaze why deploy/api
kubekaze why node            # 检查所有节点，只显示有问题的
kubekaze why node/worker-3
kubekaze sh deploy/api -c sidecar
kubekaze sh api-7d9f -- ls -l /app
kubekaze logs deploy/api -f --since 10m
kubekaze events -A --summary
kubekaze top -A --sort memory --limit 20   # 需要 metrics-server
kubekaze top ns -w --interval 2s
kubekaze rm pods -A --status Evicted --dry-run
```

退出码：`0` 正常，`1` 错误，`2` 命令、参数或选项有误，`3` 发现问题（`status`、`why`）。`sh` 会透传远端命令的退出码。

全局选项：`--kubeconfig`、`--context`、`-n/--namespace`、`-A/--all-namespaces`、`-o text|json`。

命令的输出和 `--help` 目前只有英文。

## 安装

在 [Releases 页面](https://github.com/beomzh/kubekaze/releases)下载与你的操作系统和 CPU 对应的压缩包（Linux、macOS、Windows；amd64 或 arm64），用 `checksums.txt` 校验后，把 `kubekaze` 二进制放到 `PATH` 中。

或者从源码构建（需要 Go 1.26 或更高版本）：

```bash
git clone https://github.com/beomzh/kubekaze.git
cd kubekaze
make build        # 生成 ./bin/kubekaze
```

发布流程与校验：[docs/RELEASING.md](docs/RELEASING.md)（英文）。Homebrew 和 krew 软件包在计划中。

## 更新

```bash
kubekaze update --check     # 是否有新版本？
kubekaze update             # 下载、校验、替换当前二进制
```

kubekaze 从不自行检查更新；只有运行 `update` 时才会访问 GitHub。如果安装在 `/bin` 或 `/usr/local/bin`，请运行 `sudo $(which kubekaze) update`。

**隔离网络：**在可联网的机器上下载 `kubekaze_<version>_<os>_<arch>.tar.gz`（Windows 为 `.zip`）和 `checksums.txt`，一起复制到服务器，然后运行 `kubekaze update --from <压缩包>`。整个过程不下载任何内容，会先用 `checksums.txt` 校验压缩包。**内部镜像站：**`kubekaze update --url https://mirror.example.com/kubekaze`（目录结构见 [docs/RELEASING.md](docs/RELEASING.md)）。由 Homebrew 或 krew 管理的安装请用 `brew upgrade` / `kubectl krew upgrade`。

v0.1.1 之前的版本没有 `update` 命令：请先手动安装一次 v0.1.1 或更高版本，之后使用 `update`。

## Shell 自动补全

```bash
kubekaze completion install      # 根据 $SHELL 识别 bash、zsh 或 fish，在主目录写入一个文件
kubekaze completion bash         # 或只打印脚本：bash、zsh、fish、powershell
```

`completion install` 只在主目录写入文件（不改动其他内容）：

| Shell | 文件 | 说明 |
|-------|------|------|
| bash | `~/.local/share/bash-completion/completions/kubekaze` | 需要 `bash-completion` 软件包；首次使用时加载 |
| zsh | `~/.local/share/zsh/site-functions/_kubekaze` | 该目录必须在 `fpath` 中；命令会打印需要添加到 `~/.zshrc` 的内容 |
| fish | `~/.config/fish/completions/kubekaze.fish` | 自动加载 |
| PowerShell | 无 | 将打印出的那一行添加到 `$PROFILE` |

想用自己的启动文件？不运行 `install`，在 `~/.bashrc`（或 `~/.zshrc`）中加入 `source <(kubekaze completion bash)` 即可。

使用 `alias kk=kubekaze`？Shell 按命令名绑定补全，所以需要告诉 kubekaze 这个别名：`kubekaze completion install --alias kk`。`--alias` 只负责接上补全，并不会创建 `kk` 命令，请先自行定义（在 `~/.bashrc` 中写 `alias kk=kubekaze`，或建立符号链接）。它等同于 `complete -F __start_kubekaze kk`；对 bash 和 fish 还会写一个以别名命名的小文件（`.../completions/kk`、`kk.fish`），因为它们按你输入的名字加载补全文件。也可以在 `~/.bashrc` 中加入 `source <(kubekaze completion bash --alias kk)`。

可补全命令、选项、context、命名空间、Pod 和工作负载（`deploy/...`），内容从当前指向的集群实时查询。命令和选项拼错时会提示 "Did you mean"，每个命令的 `--help` 都有详细说明和示例（退出码和环境变量见 `kubekaze --help`）。

## 适用环境

使用标准 kubeconfig 和 client-go 认证，因此同一个工具适用于虚拟机/kubeadm 集群、EKS、AKS、GKE、OpenShift 以及 Rancher 管理的集群。多集群并行执行（`--context 'prod-*'`）在计划中。

## 支持平台

单个自包含的二进制文件（不需要 `kubectl`、`oc` 或 shell），支持 Linux、macOS 和 Windows 的 amd64 与 arm64。

## 原则

默认只读 · 无 agent、无服务端、无遥测 · 最小权限 RBAC · 从不打印机密值 · 便于接管道（`-o json`）· 规则优先，AI 可选。

## 安全

参见 [SECURITY.zh-CN.md](SECURITY.zh-CN.md)。请通过 GitHub 的 "Report a vulnerability" 私下报告漏洞。

## 文档

- [产品理念](docs/CONCEPT.zh-CN.md)
- [发布与打包（英文）](docs/RELEASING.md)
- [安全策略](SECURITY.zh-CN.md)

## 许可证

基于 [Apache License 2.0](LICENSE) 授权。另见 [NOTICE](NOTICE)。

Kubernetes 是 The Linux Foundation 的注册商标。本项目与 Kubernetes 项目、Red Hat、AWS、Microsoft、Google、SUSE 均无隶属关系，也未获其背书。
