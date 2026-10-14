# KeePlus / auto-dba v2.3.7

相对 [v2.3.6](changelog-v2.3.6.md) 的增强与修复。

## Nacos · OpenSSH 私钥热更

- 识别 `openssh-key-v1` 魔数，正确包成 `BEGIN OPENSSH PRIVATE KEY`，避免误走 PKCS#8 导致 `asn1 … pkcs8` 热更失败。
- 头与正文类型不符时可按正文重包；加密口令钥仍不支持热更。
- 详见 [nacos-config.md](nacos-config.md)、[terminal.md](terminal.md)。

## StarRocks · 标记就绪

- `POST /sr-cluster/clusters/:id/mark-ready`：将 `failed`（或已有节点的 `draft`）标为 `ready`；默认 FE 探活，`force=true` 跳过。
- 集群详情「标记就绪」按钮；**不等于**重新初始化（不停服/不装包）。
- 明确：启停失败不改写集群「初始化失败」标签。详见 [sr-cluster.md](sr-cluster.md)。

## 工单

- 提交目标校验：只需实例/库已登记且属当前租户；**提交人不必**在审批人名单中（所选审核人仍须在名单内，有配置时）。

## 说明

- 终端 Permission denied 排障见 [terminal.md](terminal.md)。
