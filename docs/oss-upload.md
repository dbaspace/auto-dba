# 配置中心 · 对象存储（OSS 分片上传）

路径：`/config/oss`（菜单「对象存储」，与连接器 / 平台 Agent 同属接入组）。

## 能力概览

| 能力 | 说明 |
|------|------|
| 存储配置 | 按环境登记 S3 兼容 Endpoint / Bucket / Prefix / AK·SK（Secret 加密入库），连通性测试 |
| 分片上传 | 浏览器按片 `PUT` 到平台，平台转发为 OSS **Multipart Upload**（自研 SigV4，无 AWS/MinIO SDK） |
| 断点续传 | 任务表登记已成功 `part_number + etag`；刷新后选同文件（同 SHA-256）只补传缺失片 |
| 后台校验 | 片齐后 `CompleteMultipart`，再 **流式 GetObject 算 SHA-256** 与前端预计算比对，通过才标成功 |

默认经**平台中转**（免 OSS CORS、鉴权与菜单权限一致）。平台不落整包磁盘，分片直转 OSS。对象键为 `{prefix}/{文件名}`（例如 prefix=`starrocks` → `starrocks/foo.tar.gz`），不再自动追加 `uploads/日期`。

## S3 兼容说明

支持阿里云 OSS、腾讯云 COS、华为云 OBS、AWS S3、MinIO 等 SigV4 端点。公有云（COS / OSS / OBS / AWS）使用 **virtual-host**（`https://{bucket}.{endpoint}/key`）；`s3_compatible`（MinIO 等）使用 **path-style**（`https://{endpoint}/{bucket}/key`）。Region 从 Endpoint 主机名推断；无法识别时默认 `us-east-1`。

连通性测试对 Bucket 发 `HEAD`（path / virtual 各试一次）；鉴权通过即视为成功（桶不存在时也可能返回成功提示，请核对桶名）。

## 分片与续传

- 默认分片 **16 MiB**（创建任务时写入 `chunk_size`，此后固定；末片可更小）。
- S3 要求非末片 ≥ 约 5 MiB；后端会将分片夹在 5～100 MiB。
- 状态：`uploading` → `completing` → `verifying` → `success` / `failed`；可 `abort` 清理 Multipart。
- 前端用流式 SHA-256 预哈希；同哈希未完成任务会自动续传。
- 单片经平台转发时，请保证 `http.write_timeout` 足够（示例配置 120s；慢网可调大）。

## 完整性校验

合并完成后后台从 OSS 流式下载计算 SHA-256（并核对长度）。**约 3GB 对象的校验耗时与完整下载同量级带宽**，属预期行为。哈希或长度不符 → `failed`。

## API（需登录）

配置仍走 `/api/v1/oss-settings`。

上传：

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/oss-uploads` | 创建任务并 InitMultipart |
| GET | `/oss-uploads` `/oss-uploads/:id` | 列表 / 详情（含已完成 parts） |
| PUT | `/oss-uploads/:id/parts/:n` | 上传第 n 片（二进制 body） |
| POST | `/oss-uploads/:id/complete` | 入队后台 Complete + 校验 |
| POST | `/oss-uploads/:id/abort` | 取消并 AbortMultipart |

## 本期不做

- 浏览器直传 OSS 预签名（可二期减平台带宽）
- 与「数据备份存储池」强绑定（配置独立；SigV4 实现可后续抽取共享）
