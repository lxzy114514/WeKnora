# 课题三需求追溯

本表把课题三验收项映射到当前 0828 主线、选择性迁移增量、自动测试和真实运行证据。`passed` 表示合同和证据均闭合；`partial` 表示能力存在，但真实环境的严格证据条件尚未全部满足。

核心 RAG 指标统一使用 `metric_contract_version=1`。改变指标公式、passage-ID 映射、分词或文本预处理时必须升版本并重做 deterministic baseline 与 live pilot。

| 要求 | 状态 | 产品/数据落点 | 验证与证据 | 仍有限制 |
| --- | --- | --- | --- | --- |
| T3-A1 可复现 RAG 评测 | passed | `dataset/manifests/baseline-v1.json`、`internal/application/service/evaluation.go`、`cmd/evalctl` | 清单哈希/篡改拒绝、配置指纹、逐题证据与恢复测试；`artifacts/migration-validation-summary.json` | 真实 reference 仍需按下述晋升流程批准 |
| T3-A2 模型调用、用量与费用 | passed with unknown-cost disclosure | `model_calls`、五类模型 observability wrapper、`GET /api/v1/models/usage` | Repository/Service/API 定向测试；RAG 与 Wiki 逐调用 DB 证据；价格覆盖率测试 | 本机模型价格未配置，金额只能为 `unknown`；estimated zero 不算成本事实 |
| T3-A3 回归 CI 门禁 | passed locally | `cmd/evalbench`、`cmd/evalgate`、`config/evaluation-gate*.json`、`.github/workflows/evaluation-regression.yml` | deterministic gate 通过，故意退化 Recall 时退出码非零；门禁拒绝 `metric_eligibility`；真实 RAG raw gate 保存 | 远端 required-check 设置需仓库管理员；当前 live gate 因时延失败，成本项无资格 |
| T3-A4 缓存工程与冷/暖证据 | partial | `embedding_cache`、Embedding wrapper、普通 RAG 稳定前缀与 provider cache usage | 12 项 `-race` 合同测试；`artifacts/migration-live-cold-warm-comparison.json` | Embedding 0/5→5/5、Chat read 0→1024 已证明；暖跑仍有 113 miss tokens，严格状态 `not_comparable`；费用 `unknown` |
| T3-S1 Wiki 业务切入口 | partial | 既有 Wiki ingest 与语义化 `wiki_*` purpose；未新建平行实现 | `artifacts/migration-wiki-cold-warm-evidence.json` 保存两个 knowledge ID、时间窗和逐调用事实；read tokens 2560→6784 | 异步调用 correlation ID 为空；调用形状变化、暖跑 miss 12078、费用未知，不能声称端到端改善 |
| 可选解析器横评 | partial by design | `dataset/parser_benchmark/v1`、`cmd/topic3-parser-benchmark` | 跨平台重建后的独立 E1 复审；固定 8×7/56 单元报告；Windows/Linux 同哈希重建；`go test -race ./internal/evaluation/...` | 本机仅 `simple` ready，结果为 1/8，不冒充八引擎基线 |

## Reference bundle 晋升规则

确定性 reference bundle 由冻结用例、审批记录中绑定的 `metric_contract_version=1`、当前代码生成的便携报告、阈值与复核记录组成；当前正式入口是 `testdata/evaluation/deterministic-baseline.json` 和 `config/evaluation-gate-deterministic.json`。指标版本暂不作为 portable JSON 的强制字段，以保持历史诊断报告可读取。

真实 RAG bundle 必须同时满足：非空且相同的配置哈希、明确代码版本、同指标合同、完整逐题证据、同条件重复运行、价格覆盖口径明确、人工/审核任务复核完成。批准后才可写入 `testdata/evaluation/live-baselines/`。本轮 RAG 与 Wiki 报告是诊断证据：它们证明真实缓存使用，但分别因 provider 新计算、调用形状变化、价格未知或时延失败而没有晋升为 reference。

最终工程闭环以提交 `c88047bf0285a1ded72f06ff038f3c1679b2ef2d` 为被测代码：Linux `go test -p 2 ./...`、全仓 `go vet -p 2 ./...`、关键路径竞态测试、确定性 4 项质量门禁和生产镜像健康检查全部通过。机器可读入口为 `artifacts/migration-validation-summary.json`。
