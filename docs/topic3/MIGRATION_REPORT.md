# 课题三选择性迁移报告

## 范围与原则

目标分支为 `feat/topic3-evaluation-cost-cache-20260828`。迁移只把第二套实现中能补强当前方案、且与当前架构边界兼容的内容移入；没有 cherry-pick、rebase、整分支 merge，也没有修改远端仓库。

保留 0828 的评测运行、模型调用、Embedding 缓存和 CI 主链作为唯一实现。第二套实现的六表调用记录模型、runtime factory、另一套状态机和预检框架未迁入，以避免重复事实来源及额外改造成本。

## 已迁入内容

1. 七文档解析语料、版本化抽取规则、五项质量真值和确定性重建工具。
2. 独立 E1 审核链：实现者渲染、审核者逐页/逐文件核验、四类哈希和收据绑定。第一次审核发现确定性测试把审核 envelope 当作语料 payload 比较；修正契约后由同一独立审核任务复审通过，拒绝与通过收据均保留。
3. 文档解析器运行器：固定八引擎、七文档、56 单元矩阵；记录请求/实际引擎、fallback、超时、失败和 unavailable/unsupported；输出 JSON 与 Markdown。
4. 对现有缓存主线的聚焦回归证据：Embedding 批量复用与并发合并、隔离键、普通 RAG/Wiki 稳定前缀、DeepSeek cache usage 解析。
5. 核心指标合同、reference bundle 晋升条件和 T3-A1～A4/T3-S1 需求追溯。
6. 成本门禁资格修复：报告携带价格覆盖率；缺价产生的 estimated zero 进入 `metric_eligibility`，不再被门禁当作有效数值通过。

## 明确未迁入内容

- 第二套六表调用记录模型、供应商 attempt 状态机和 runtime factory。
- 与 0828 现有模型观测/评测表重复的 API、DTO、迁移和前端统计页面。
- 依赖未知外部价格或凭据才能声称成立的费用数字。
- 任何远端分支保护、Secret、PR、Tag 或发布操作。

## 运行结果

解析器基准在当前机器完成一次正式本地运行：`1/8` 引擎 ready，56 个单元中 2 个计分、35 个 unavailable、19 个 unsupported、0 个失败，结论为 `partial`。只有 `simple` 引擎可用，因此该报告不是完整八引擎基线。

缓存契约的 12 项聚焦竞态测试全部通过。Docker 恢复后又用当前提交镜像完成了同配置冷/热 RAG 对比：Embedding 命中从 0/5 升至 5/5，Chat cache-read tokens 从 0 升至 1024，检索指标不变。暖跑仍有 113 个 Chat cache-miss tokens，故严格可比状态是 `not_comparable`；完整门禁因远端生成时延增加而失败。模型价格为空，零费用是 estimated 而非账单事实，原始 gate 的零成本“通过”有效解释为 `inconclusive`。

Wiki 场景也完成两轮真实运行。`wiki_*` 调用的 cache-read tokens 从 2,560 增至 6,784、读取比例从 0.188415 增至 0.359665，证明缓存用于生产链路；但调用形状随 Wiki 状态变化、暖跑仍有 12,078 个 cache-miss tokens、价格未知，因此同样按 `partial / not_comparable` 披露。异步调用 correlation ID 为空的现场差异没有被掩盖，报告保留 knowledge ID 与受控时间窗作为绑定证据。

## 证据入口

- E1 审核：`docs/topic3/audit/E1_b43beccd_CROSS_PLATFORM_REAPPROVAL_RECEIPT.md`
- 解析器报告：`artifacts/parser-benchmark-report.json`、`artifacts/parser-benchmark-report.md`
- 缓存确定性证据：`artifacts/migration-cache-deterministic-evidence.json`
- 真实冷/热状态：`artifacts/migration-cache-live-status.json`
- 真实冷/热对比：`artifacts/migration-live-cold-warm-comparison.json`
- Wiki 冷/暖逐调用证据：`artifacts/migration-wiki-cold-warm-evidence.json`
- 需求追溯：`docs/topic3/REQUIREMENT_TRACEABILITY.md`

## Git 检查点

- `7ac687a0`：冻结迁移前已有的 0828 评测证据。
- `c15bbcc4`：迁入解析语料、指标与 E1 基础。
- `c38f748d`：增加真实 parser factory 驱动的 56 单元运行器。
- `4edbde49`：修正审核 envelope 与确定性语料 payload 的边界。
- `8622e455`：提交独立审核通过的语料和收据。
- `db7ca387`：保存迁移测试证据和交付状态。
- `cd94c57d`：保存真实 RAG 冷/暖缓存证据。
- `e0174424`：补齐 Wiki 真实证据、parser 安全/失败语义、成本门禁资格与最终双轴审查修复。
- `c88047bf`：修复 parser 跨 Go/OS 字节漂移与 CRLF 测试辅助函数，保存三轮 Linux 全仓测试和新的 E1 独立复审证据。

最终提交号以 `git rev-parse HEAD` 为准；本文不把迁移过程中的中间提交冒充最终版本。

## 最终交付验证

Linux amd64、Go 1.26 环境的 `go test -p 2 ./...` 已全仓通过；`go vet -p 2 ./...`、关键路径 `-race` 测试和当前代码生成的 4 项确定性质量门禁均通过。生产镜像 `sha256:aa53e1d16c3554b5f06f2dff9ce07fff0cc158601f496daa2267da4a32f22267` 已运行，嵌入提交号为 `c88047bf0285a1ded72f06ff038f3c1679b2ef2d`，app 容器 healthy，后端与前端 HTTP 检查均返回 200；PostgreSQL migration version 为 90、`dirty=false`，四张课题表均存在。

语料编码修复后重新执行 parser 矩阵，状态仍为 `partial`、ready 引擎 `1/8`、计分单元 `2/37`，但报告现在绑定独立复审通过的新语料哈希。RAG/Wiki 的真实运行结论没有被测试成功覆盖：其严格状态仍分别保留为 `partial / not_comparable`，成本仍为 `unknown`。

交付前补充的一次性 DeepSeek 货币层实验没有改写上述历史结论。它在官方谷价时段内证明新 chat 调用可由价格快照产生正 USD 金额，Summary 为 USD 0.003405424；本地 embedding 的 provider 计算仍无可靠费率，旧 RAG/Wiki 调用也没有回填。临时模型价格已通过管理 API 恢复为空，完整证据见 `artifacts/pricing-currency-evidence-20260912.json`。
