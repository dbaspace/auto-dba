# KeePlus / auto-dba v1.12.69

相对 [v1.12.68](changelog-v1.12.68.md)。无表结构变更。

## CDC 源端生效地址

- **只展示一个地址**：列表、详情和修复弹窗显示任务实际使用的源端，格式为「实例名 — 生效地址」。优先级：任务 `source_host` > Debezium `database.hostname` > 实例 host。
- **修复不再改写实例地址**：同一实例下不同库可能走不同域名。修复源端只把新域名追加到实例域名列表并同步访问域名，不改实例 `host` / `port`。Connector 更新、配置版本和重启不变。
- **一并修复收紧**：默认不勾选。勾选后只处理同实例、且修复前生效地址与当前任务相同的其它运行中初始化任务。
- **源库连接跟随任务地址**：加表、移表、快照信号、清理、重新初始化及其预览、状态探测、表结构巡检、可选表和 Publication 查询，在任务记有 `source_host` 时用该地址连接。未修复过的任务仍用实例地址。

## 打包与发版

```bash
STAMP=v1.12.69 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.69 -m "v1.12.69"
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
