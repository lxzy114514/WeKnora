# 课题三交付总览

本目录是“评测、模型成本与缓存工程化”课题的集中交付包。实现遵循三个原则：评测条件可追溯、用量数据来自数据库、缓存优化不能牺牲隔离与正确性。

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

- [六大板块完整实现报告](SIX_MODULE_IMPLEMENTATION_REPORT.md)：从总体框架、六个板块、关键实现到真实验收、局限和术语的系统说明。
- [工程规格](SPEC.md)：范围、测试接缝、验收场景。
- [架构设计](ARCHITECTURE.md)：模块边界、数据流、数据库与隐私设计。
- [运行手册](RUNBOOK.md)：一条命令评测、门禁演示、CI 和故障恢复。
- [实验报告](EXPERIMENT_REPORT.md)：可重复的缓存与门禁证据，以及真实模型实验边界。
- [测试报告](TEST_REPORT.md)：已执行检查、结果和仓库原有环境问题。
- [实现重难点与参考资料](IMPLEMENTATION_CHALLENGES_AND_REFERENCES.md)：逐项解释为什么难、如何实现、参考依据、项目自有设计与已知边界。
- [已知限制](LIMITATIONS.md)：仍需部署环境或管理员完成的事项。
- [提交清单](submission.yaml)：机器可读的交付索引。
- [迁移报告](MIGRATION_REPORT.md)：说明从第二套实现选择性迁入的范围、验证证据和未完成的真实环境实验。

## 最短验收路径

服务和模型已经配置时，在仓库根目录执行：

```powershell
$env:WEKNORA_BASE_URL = "http://localhost:8080"
$env:WEKNORA_API_KEY = "<仅在当前终端设置，不要写入仓库>"
go run ./cmd/evalctl -dataset baseline-v1 -chat-model "<chat-id>" -embedding-model "<embedding-id>" -rerank-model "<rerank-id，可为空>" -summary-model "<summary-id>"
```

结束后会生成 `artifacts/evaluation-report.json`、`artifacts/evaluation-detail.json` 和人类可读的 `artifacts/evaluation-report.md`。详细报告包含数据集、模型、分块参数、代码版本、检索/回答指标、成本、分阶段耗时和逐题可复算证据。

> 本分支没有提交任何密钥，也没有伪造真实模型实验数字。没有可用于当前分支服务的评测凭据时，交付报告只记录确定性自动化结果。

可选解析器基准不需要启动 WeKnora 服务：

```powershell
go run ./cmd/topic3-parser-benchmark -repository-root . -run-id parser-local
```

命令始终生成 56 单元矩阵。缺少外部解析服务或凭据时，对应单元会明确记为 `unavailable`；本机已执行结果为 `partial`、可用引擎 `1/8`，见 `artifacts/parser-benchmark-report.md`。这不能被表述为完整八引擎实测。
