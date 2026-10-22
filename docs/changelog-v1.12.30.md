# KeePlus / auto-dba v1.12.30

相对 [v1.12.29](changelog-v1.12.29.md)。无表结构变更，无配置项变更。

## CDC Debezium 配置

- 任务详情「Debezium 配置」页签：查看时主按钮为「编辑」，点击后才进入编辑。
- 编辑中保留「取消」；提交按钮为「保存」（原先写成「确认保存」）。保存接口、版本历史与复制不变。
- Connector 缺失或任务处于 pending 时，「编辑」仍禁用。

## 打包与发版

```bash
STAMP=v1.12.30 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.30
```

部署见 [deploy.md](deploy.md)。
