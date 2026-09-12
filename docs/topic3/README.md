# 课题三交付总览

本目录集中提供“评测、模型成本与缓存工程化”课题的设计、实现、验证和交付证据。评测运行绑定可追溯条件，模型用量由数据库事实聚合，缓存路径保持租户隔离和结果正确性。

## 已完成能力

| 能力 | 交付结果 | 主要入口 |
| --- | --- | --- |
| 可复现评测 | `baseline-v1` 清单、文件 SHA-256、完整配置指纹、一命令运行 | `dataset/manifests/baseline-v1.json`、`cmd/evalctl` |
| 结果持久化 | 运行、配置、四类汇总、逐题结果、异常重启恢复、租户隔离 | `evaluation_runs`、`evaluation_cases` |
| 质量门禁 | 当前代码生成确定性报告；高/低优指标阈值、全部退化项输出、PR/定时/手动真实 RAG CI | `cmd/evalbench`、`cmd/evalgate`、`evaluation-regression.yml` |
| 模型成本 | Chat、Embedding、Rerank、VLM、ASR 每次调用落库，保存价格快照 | `model_calls`、`GET /api/v1/models/usage` |
| 成本页面 | 按模型和日期筛选调用数、缓存读取、Embedding 命中、估算费用 | 设置 → 模型管理 |
| Embedding 缓存 | 租户/模型/配置指纹/维度/文本哈希隔离，批量部分命中、顺序保持、并发合并 | `embedding_cache` |
| 提示词缓存 | 稳定系统前缀指纹与动态问题/上下文分离；Wiki 原有缓存路径纳入观测 | `chat_pipeline/common.go` |
| 文档解析器基准（可选） | 独立审核的七文档真值、8×7 固定矩阵、五项指标和实际 fallback 记录 | `cmd/topic3-parser-benchmark`、`dataset/parser_benchmark/v1` |

## 文档导航

- [工程设计与实现报告](SIX_MODULE_IMPLEMENTATION_REPORT.md)：总体架构、六个功能模块、关键实现、真实验收与已知限制。
- [工程规格](SPEC.md)：范围、测试接缝、验收场景。
- [架构设计](ARCHITECTURE.md)：模块边界、数据流、数据库与隐私设计。
- [运行手册](RUNBOOK.md)：一条命令评测、门禁演示、CI 和故障恢复。
- [实验报告](EXPERIMENT_REPORT.md)：可重复的缓存与门禁证据，以及真实模型实验边界。
- [测试报告](TEST_REPORT.md)：已执行检查、结果和仓库原有环境问题。
- [工程设计、实现约束与参考依据](IMPLEMENTATION_CHALLENGES_AND_REFERENCES.md)：记录工程风险、实现方式、外部依据、本地设计决策与已知边界。
- [已知限制](LIMITATIONS.md)：仍需部署环境或管理员完成的事项。
- [提交清单](submission.yaml)：机器可读的交付索引。
- [迁移报告](MIGRATION_REPORT.md)：说明从第二套实现选择性迁入的范围、真实验证证据和如实保留的限制。
- [迁移代码审查](MIGRATION_CODE_REVIEW.md)：记录双轴审查、修复项与最终结论。
- [需求追溯](REQUIREMENT_TRACEABILITY.md)：把 T3-A1～A4、T3-S1 与实现、测试和真实证据逐项对应。

## 验收步骤

服务和模型已经配置时，在仓库根目录执行：

```powershell
$env:WEKNORA_BASE_URL = "http://localhost:8080"
$env:WEKNORA_API_KEY = "<仅在当前终端设置，不要写入仓库>"
go run ./cmd/evalctl -dataset baseline-v1 -chat-model "<chat-id>" -embedding-model "<embedding-id>" -rerank-model "<rerank-id，可为空>" -summary-model "<summary-id>"
```

结束后生成 `artifacts/evaluation-report.json`、`artifacts/evaluation-detail.json` 和 `artifacts/evaluation-report.md`。详细报告包含数据集、模型、分块参数、代码版本、检索/回答指标、成本、分阶段耗时和逐题可复算证据。

> 密钥未纳入版本控制。确定性测试、真实运行和不可比结论分别记录，并使用独立的证据状态。

当前提交已完成一组同配置冷/热实跑：Embedding 命中从 0/5 升至 5/5，Chat cache-read tokens 从 0 升至 1024。暖跑仍有 113 个 Chat cache-miss tokens，因此按严格规则为 `not_comparable`；完整门禁还因远端生成时延波动失败，且该组运行发生时模型未配置价格，所以总状态为 `partial`。原始门禁中的零成本检查没有价格覆盖，实际解释为 `inconclusive`，详见 `artifacts/migration-live-cold-warm-comparison.json`。

交付前另在 DeepSeek 官方谷价时段完成一次性货币层验证。模型调试和一轮 `baseline-v1` 小规模评测产生 3 条正金额 DeepSeek 调用，时间窗 Summary 为 USD 0.003405424；评测报告自身为 USD 0.002908708，价格覆盖率为 1。该次评测的两个 Embedding 调用全部命中应用缓存，属于可证明的 provider 零计算；本地 `bge-m3:latest` 在 cache miss 时仍无可靠价格，因此整个部署仍是部分价格覆盖。价格验证后已通过管理 API 恢复为空，历史调用没有回填。完整审计链见 `artifacts/pricing-currency-evidence-20260912.json`。

Wiki 真实冷/暖运行也已完成：`wiki_*` 调用的 cache-read tokens 从 2,560 增至 6,784、读取比例从 0.188415 增至 0.359665。由于 Wiki 状态演进使调用形状变化，暖跑仍有 12,078 个 cache-miss tokens，且价格缺失，这组结果同样是 `partial / not_comparable`；它证明缓存被真实链路使用，不证明端到端费用或时延改善。精确逐调用事实见 `artifacts/migration-wiki-cold-warm-evidence.json`。

可选解析器基准不需要启动 WeKnora 服务：

```powershell
go run ./cmd/topic3-parser-benchmark -repository-root . -run-id parser-local
```

命令始终生成 56 单元矩阵。缺少外部解析服务或凭据时，对应单元明确记为 `unavailable`；本机已执行结果为 `partial`、可用引擎 `1/8`，见 `artifacts/parser-benchmark-report.md`。该结果不构成完整八引擎实测。

最终工程验证基于提交 `c88047bf0285a1ded72f06ff038f3c1679b2ef2d`：Linux 全仓测试、全仓 `go vet`、关键路径竞态测试与确定性质量门禁均通过。生产镜像已替换本地 app 容器并通过前后端 HTTP 检查；完整命令、哈希与两次失败后修复的原始日志见 `artifacts/migration-validation-summary.json` 和 `TEST_REPORT.md`。
