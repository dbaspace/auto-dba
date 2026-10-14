# KeePlus / auto-dba v2.3.6

相对 [v2.3.5](changelog-v2.3.5.md) 的补丁：修复 Nacos 更新 `terminal.ssh_private_key` 的 YAML 与热更问题。

## Nacos · SSH 私钥

- 支持生产常见的**裸 Base64**（无 `BEGIN/END`，前缀如 `MIIE…`）：物化时自动包成 `BEGIN RSA PRIVATE KEY` / `BEGIN PRIVATE KEY`。
- 误用 `BEGIN OPENSSH PRIVATE KEY` 包裹 RSA 正文时，会剥头后按 DER 重包。
- 热更按 PEM 内容指纹判断；仅改私钥也会 `nacos watch: applied`（`sections` 含 `terminal`）。
- YAML 解析失败且与私钥相关时，日志附加「须用 `|` 块标量且每行缩进」提示。

## 运维注意

- Nacos 中 `ssh_private_key: |` 后**每一行**须缩进；顶格 Base64 会导致 `could not find expected ':'`。
- 详见 [nacos-config.md](nacos-config.md)「SSH 私钥」、[terminal.md](terminal.md)。
