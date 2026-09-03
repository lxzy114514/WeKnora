# 课题三真实评测验收记录（2026-09-03）

## 1. 结论

本地 Docker 环境完成了一次真实、单题的 RAG 评测闭环：固定数据集通过完整性校验，临时知识库完成索引，检索找回全部相关文档，DeepSeek 生成答案，评测结果与模型调用记录持久化，最后自动软删除临时知识库。

本次运行可作为候选基线的人工审查材料，但不应仅凭一次、一道题的结果直接晋升为 CI 的正式真实基线。

## 2. 运行身份

- 分支：`feat/topic3-evaluation-cost-cache-20260828`
- 任务 ID：`evaluation_10000_1788438818252_98b36181_baselinev1`
- 数据集：`baseline-v1`，版本 `1`
- 数据集哈希：`f3369dfc145dfc0e3552542d3985911b36cd8dfc6cd95af43bc31284cccb193a`
- 配置哈希：`689bd0ea6add9ac43005e2511b729db7c78e33500f542267a6c63ca288a22f77`
- 开始时间：`2026-09-03T12:33:38Z`
- 结束时间：`2026-09-03T12:33:57Z`
- 状态：成功，`1/1`

原始产物：

- `artifacts/manual-live-evaluation-report.json`：适合 CI 比较的扁平指标。
- `artifacts/manual-live-evaluation-detail.json`：完整配置、阶段耗时和逐题证据。
- `artifacts/manual-live-evaluation-report.md`：适合人工阅读的报告。
- `artifacts/manual-deterministic-report.json` 与 `artifacts/manual-deterministic-gate.json`：无需外部模型的确定性门禁证据。

## 3. 主要结果

### 检索质量

`Recall`、`Precision`、`MRR`、`MAP`、`NDCG@3`、`NDCG@10` 均为 `1.0`。在这道题上，正确资料全部被找回且排序有效。

这只能证明当前固定样例通过，不能推导所有问题都能达到满分。正式结论需要扩大数据集并重复运行。

### 回答质量

- ROUGE-L：`0.25`
- ROUGE-1：`0.277778`
- BLEU-1：`0.116505`

这些指标衡量生成答案与参考答案的词语或片段重合度，不等价于百分制成绩。实际答案覆盖 UNIX、Linux、FreeBSD、Mac OS、DOS，并额外提到了 Chrome OS；内容较参考短答案更详细，因此字面重合指标不高。

### 耗时

- 总耗时：`18,900 ms`
- 建库索引：`5,051 ms`
- 单题平均：`13,783 ms`
- 检索：`857 ms`
- 生成：`12,851 ms`
- 重排：`0 ms`（本次没有配置 Rerank 模型）

生成阶段占主要时间，符合远程大模型调用通常比本地检索慢的现象。

### 模型调用与缓存

数据库记录了三次模型调用：

1. 对 4 段语料生成向量：`4,711 ms`，4 个缓存未命中。
2. 对 1 个问题生成向量：`159 ms`，1 个缓存未命中。
3. DeepSeek 回答：输入 `1,137` tokens，输出 `609` tokens，总计 `1,746` tokens，`12,834 ms`。

这是首次以当前模型指纹运行，因此 5 个 Embedding 项目均未命中缓存。后续在租户、模型配置、维度与文本不变时重复运行，才应观察缓存命中。

### 成本口径

报告记录三次调用，但成本为 `$0`，数据库同时标记 `cost_estimated=true`。这表示当前 WeKnora 配置没有算出非零价格，不能据此断言远程厂商账单真的为零。要得到可用成本数字，需要在模型配置中填写厂商的输入/输出 token 单价。

## 4. 清理与安全证据

- 本次临时知识库 ID：`3518007e-1473-4ee4-8349-224523620ea6`。
- 它在任务完成后同一秒写入 `deleted_at`，说明自动清理成功；数据库保留的是软删除审计痕迹。
- 三份真实评测报告未匹配到 API Key、Bearer、Authorization 或 App Secret 模式。
- 演示用 API Key 应在运行后从终端环境清除，并在“API 集成”页面吊销。

## 5. 演示发现并修复的问题

第一次从本地 Compose 构建的报告显示 `code_version=unknown`。原因是 Docker 构建上下文按安全惯例排除了 `.git`，而评测模块没有使用生产构建已经注入的 Commit ID。

本分支随后用测试驱动方式补上：生产注入的 Commit ID 优先，Go VCS 信息作为本地构建兜底；Docker 镜像同时补打包 `dataset/manifests`，避免运行时只存在 Parquet 文件却缺少完整性清单。原始报告保持不变，作为发现问题的真实证据。

## 6. 门禁结果与后续动作

确定性门禁通过 `4` 项检查。真实报告尚未晋升为正式基线，正确流程是：固定相同配置重复运行、人工检查逐题差异与成本、再把经过评审的便携 JSON 放入 `testdata/evaluation/live-baselines/<config_hash>.json`。

与评测、指标、门禁和三个 CLI 相关的定向测试通过。扩大到整个 `internal/application/service` 包时，Windows 环境仍有 4 个非课题三失败：3 个 SQLite 临时数据库文件在测试清理时被占用，1 个不可读脚本测试受到 Windows 文件权限语义影响。这些失败与本次版本注入和 Docker 数据集清单改动没有调用关系，本验收记录不把它们计作通过，也未扩大范围修改。
