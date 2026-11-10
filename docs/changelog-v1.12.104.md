# KeePlus / auto-dba v1.12.104

相对 [v1.12.103](changelog-v1.12.103.md)。同步列表的任务名改为 publication。查询窗口的字段提示带上注释，候选列表不再被编辑区裁切。无 Agent 协议变更，无表结构变更。

## 同步槽

- 「任务名」显示 publication 名。分库绑定用各自的 publication；没有 publication 时用 connector 名。不再显示 CDC 任务记录上的任务标题。
- 任务名筛选和批量重启确认用同一份名字。重启仍然按 connector 发起，任务名只负责展示和勾选确认。

## 数据库查询

- 字段候选项写成 `字段名-注释`。没有注释时只显示字段名。重名列仍在后面带上表名或别名。
- 光标在字段位置、字段还在读取时，不先用关键字把列表占满，只显示「读取字段」。
- 候选列表挂到页面上，跟着光标，并避开窗口边缘，不再被查询编辑区裁掉。
- 候选打开时，Enter 和 Tab 都填入当前项。没有候选项时 Enter 仍是换行。`Ctrl / ⌘ + Enter` 仍是运行。

## 打包与发版

```bash
STAMP=v1.12.104 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.104 -m "v1.12.104"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。同步列表的任务名要使用本版平台进程，请重启平台后再看。查询窗口的提示刷新页面即可。
