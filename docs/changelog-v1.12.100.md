# KeePlus / auto-dba v1.12.100

相对 [v1.12.99](changelog-v1.12.99.md)。Agent管理三个页签整理工具栏。SQL 执行器列表用运行状态代替启用列。无 Agent 协议变更，无表结构变更。

## Agent 管理

- 平台 Agent、Node Agent 的工具栏改为先筛环境和地址，再放刷新、下发和手工登记。平台 Agent 页去掉标题和归属说明。
- SQL 执行器列表去掉「本服务所在环境」和「新增执行器」。新执行器仍由上方下发登记，已保存的本机直连环境不变，这个页面不再修改它。
- SQL 执行器「启用」列改为「状态」。打开页面后先显示检测中，探活结束后标记运行中、已停止或未知。编辑里关掉启用的显示已禁用。

## 打包与发版

```bash
STAMP=v1.12.100 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.100 -m "v1.12.100"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。只改了页面，刷新即可，平台进程不必重启。
