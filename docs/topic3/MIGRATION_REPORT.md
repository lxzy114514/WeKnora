# 课题三选择性迁移报告

## 范围与原则

目标分支为 `feat/topic3-evaluation-cost-cache-20260828`。迁移只把第二套实现中能补强当前方案、且与当前架构边界兼容的内容移入；没有 cherry-pick、rebase、整分支 merge，也没有修改远端仓库。

保留 0828 的评测运行、模型调用、Embedding 缓存和 CI 主链作为唯一实现。第二套实现的六表账本、runtime factory、另一套状态机和预检框架没有迁入，因为它们会与当前已经可运行的四表方案形成重复事实来源和过高改造成本。

## 已迁入内容

1. 七文档解析语料、版本化抽取规则、五项质量真值和确定性重建工具。
2. 独立 E1 审核链：实现者渲染、审核者逐页/逐文件核验、四类哈希和收据绑定。第一次审核发现确定性测试把审核 envelope 当作语料 payload 比较；修正契约后由同一独立审核任务复审通过，拒绝与通过收据均保留。
3. 文档解析器运行器：固定八引擎、七文档、56 单元矩阵；记录请求/实际引擎、fallback、超时、失败和 unavailable/unsupported；输出 JSON 与 Markdown。
4. 对现有缓存主线的聚焦回归证据：Embedding 批量复用与并发合并、隔离键、普通 RAG/Wiki 稳定前缀、DeepSeek cache usage 解析。

## 明确未迁入内容

- 第二套六表调用账本、供应商 attempt 状态机和 runtime factory。
- 与 0828 现有模型观测/评测表重复的 API、DTO、迁移和前端统计页面。
- 依赖未知外部价格或凭据才能声称成立的费用数字。
- 任何远端分支保护、Secret、PR、Tag 或发布操作。

## 运行结果

解析器基准在当前机器完成一次正式本地运行：`1/8` 引擎 ready，56 个单元中 2 个计分、35 个 unavailable、19 个 unsupported、0 个失败，结论为 `partial`。只有 `simple` 引擎可用，因此该报告不是完整八引擎基线。

缓存契约的 12 项聚焦竞态测试全部通过。真实冷/热 RAG 对比未完成：Docker Desktop 因本地 Secrets Engine 套接字异常无法启动 Linux engine。没有创建评测任务、没有发出供应商调用、没有产生费用；状态按事实记录为 `inconclusive`。

## 证据入口

- E1 审核：`docs/topic3/audit/E1_4edbde49_ROUND2_REVIEW_RECEIPT.md`
- 解析器报告：`artifacts/parser-benchmark-report.json`、`artifacts/parser-benchmark-report.md`
- 缓存确定性证据：`artifacts/migration-cache-deterministic-evidence.json`
- 真实冷/热状态：`artifacts/migration-cache-live-status.json`

## Git 检查点

- `7ac687a0`：冻结迁移前已有的 0828 评测证据。
- `c15bbcc4`：迁入解析语料、指标与 E1 基础。
- `c38f748d`：增加真实 parser factory 驱动的 56 单元运行器。
- `4edbde49`：修正审核 envelope 与确定性语料 payload 的边界。
- `8622e455`：提交独立审核通过的语料和收据。

最终提交号以 `git rev-parse HEAD` 为准；本文不把迁移过程中的中间提交冒充最终版本。
