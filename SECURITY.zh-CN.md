# 安全策略

[English](SECURITY.md) | [한국어](SECURITY.ko.md) | 简体中文 | [日本語](SECURITY.ja.md)

> 状态：预发布。下述模型是首个版本的承诺。标有*（计划中）*的项目尚未实现。如与英文版不一致，以英文版为准。

## 报告漏洞

安全问题请**不要**公开提交 issue。

请使用 GitHub 的私密报告功能：本仓库的 **Security → Report a vulnerability**。请附上版本、环境（发行版、Kubernetes 版本）和复现步骤。我们力争在 5 个工作日内确认收到，并尽快为已确认的问题提供修复或缓解措施；如报告者愿意，会予以署名致谢。

## 受支持的版本

1.0 之前只有最新的次版本获得安全修复；1.0 之后为最新的两个次版本。

## 安全模型

| 方面 | 承诺 |
|------|------|
| 凭据 | 使用你现有的 kubeconfig 及其 exec 认证插件。从不保存、复制、记录或传输凭据和令牌。 |
| 默认只读 | `status`、`why`、`logs`、`changes` 只执行 get/list/watch 和日志读取。 |
| 变更操作 | `sh`（pods/exec）、临时调试容器等操作需显式发起、在输出中标明，并在被识别为生产环境的 context 上要求确认。 |
| 批量删除 | `rm` 总是先显示预览，确认后才删除；每个 Pod 按 UID 删除，同名重建的 Pod 不受影响；10 个及以上需要输入数量；类似生产环境的 context 需要输入 context 名称，脚本中则用 `--yes --confirm-context=<name>`；即使带 `--yes`，`--max`（默认 50）仍限制单次数量；逐个对象报告结果。 |
| 写入 kubeconfig | `ctx` / `ns` 只修改 `current-context` 和 context 的 `namespace` 字段，使用 client-go 的标准写入器（与 `kubectl config use-context` 相同）。kubeconfig 的其他内容不动；记住的“上一个”值保存在仅用户可读的缓存文件中（权限 0600）。 |
| Secret | 从不打印 Secret 的 `data`。来自 Secret 的环境变量值会被遮蔽。ConfigMap/Secret 的变更只以哈希报告。 |
| 日志脱敏 | 默认遮蔽输出中常见的令牌和密码模式；必须显式传入 `--no-redact` 才会关闭。 |
| 更新 | `kubekaze update` 仅在你要求时运行，从不在后台运行。要求压缩包与发布页 `checksums.txt` 中的 SHA-256 一致，替换前先试运行一次新二进制，用单次原子重命名完成替换（任何失败都保留旧二进制），只接受 https（仅 localhost 允许明文 http）。校验和只能证明下载内容完整，不能证明由谁构建：请自行验证 `checksums.txt` 的 cosign 签名（见下文），尤其是在把文件带入离线网络之前。 |
| 遥测 | 无。除你指向的 Kubernetes API server 外不发起网络调用（仅当你开启可选的 AI 功能时才访问 LLM 端点）。 |
| AI 功能*（计划中）* | 默认关闭。只向你配置的端点发送已脱敏、用户可预览（`--dry-run`）的数据；支持本地模型。 |
| 调试镜像 | 临时调试容器不具备特权（禁止提权、丢弃全部 capability、默认 seccomp）。镜像可配置，默认按摘要固定；拒绝 `latest` 和不带标签的镜像。 |
| 最小权限 | 记录每个命令所需的 RBAC；缺少权限时如实报告，不做绕过。 |
| 终端安全 | 集群返回的字符串（事件、日志、名称）中的控制字符会被转义，防止终端转义序列注入。 |
| 多集群*（计划中）* | 并行执行时每个 context 使用各自的凭据；结果不会跨凭据混合。 |

## 供应链

- 发布版本由 GitHub Actions 从打了标签的提交构建，Action 按提交 SHA 固定。
- 制品附带 SHA-256 校验和、已签名的校验和文件（cosign keyless）、SBOM（SPDX）和 SLSA 构建来源证明。
- 依赖由 Dependabot 跟踪，并用 `govulncheck` 扫描。
- 可复现构建：`-trimpath`、静态二进制（`CGO_ENABLED=0`）、版本信息通过 ldflags 注入。

## 校验发布版本

每个发布版本都包含 `checksums.txt` 和 cosign keyless 签名包 `checksums.txt.sigstore.json`。将它们与所用平台的压缩包一起下载（示例使用 v0.1.0），然后：

```bash
# 1. 压缩包与校验和列表一致
shasum -a 256 -c checksums.txt --ignore-missing      # Linux: sha256sum -c checksums.txt --ignore-missing

# 2. 校验和列表由本仓库的发布工作流签名
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/beomzh/kubekaze/\.github/workflows/release\.yml@refs/tags/v0\.1\.0$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

每个压缩包还附有 SPDX SBOM（`*.sbom.json`）。
