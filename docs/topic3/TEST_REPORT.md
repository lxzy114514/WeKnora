# 测试报告

更新日期：2026-09-12

## 已通过

- 数据集版本、哈希、路径安全、篡改拒绝。
- 评测运行/逐题 Repository、租户隔离、异常重启恢复。
- 模型调用落库、时间/模型/用途筛选、费用计算。
- Chat 流式与非流式只记录一次；Embedding、Rerank、VLM、ASR 观测装饰器。
- Embedding 完全/部分命中、顺序保持、单条与批次并发合并、租户/模型/配置指纹/维度隔离，以及命中统计失败时仍返回缓存值。
- 普通 RAG 稳定提示词前缀，以及 Wiki 已有缓存元数据测试。
- 回归门禁边界、全部失败项、配置哈希缺失/不一致、空策略和重复指标拒绝、CLI 退出码。
- SQLite 迁移版本及四张新表。
- 模型用量 API 的租户、模型、用途和 RFC3339 时间筛选。
- 前端 TypeScript 类型检查、四种语言键一致性、Vite 生产构建。
- 当前代码确定性报告生成、4 项严格回归比较，以及故意降低 Recall 的失败路径。
- 线上与确定性评测共享 12 项指标注册表；注册表名称唯一且字段读写一致。
- 流式 Chat 错误事件记为失败，同时保留已收到的 usage；请求取消后用有界独立上下文保存失败观测；缓存读/写/未命中成本不重复计费。
- 逐题检索/重排证据、答案与阶段耗时持久化；重复文本优先使用 chunk 中的稳定 passage 元数据映射。
- 模型连通性测试也进入统一观测；旧客户端省略新定价字段时保留已有价格，显式空对象仍可清除。
- 旧版知识库内联 VLM 的 OCR/Caption 两次调用均记录，并使用不随密钥轮换变化的稳定模型 ID；VLLM 价格输入/输出可保存和回填。
- 状态轮询不加载逐题答案，CLI 完成后按最多 100 条分页拉取证据；205 条用例验证会发出 3 次有界请求。

## 环境相关的非本功能失败

在 Windows 执行仓库较大范围测试时观察到以下既有环境问题：

- 部分 SQLite 测试清理临时数据库时文件仍被占用。
- Docker 主机使用 `npipe` 时，若测试路径只接受 Unix scheme 会失败。
- `internal/container` 测试在当前 MinGW 与 DuckDB 静态库组合下链接失败（`__emutls` 符号）。
- Windows 原生 `go build ./...` 也会在 `cmd/download/duckdb`、`cmd/desktop` 和 `cmd/server` 触发同一个 DuckDB/MinGW `__emutls` 链接错误；Linux 生产 Docker 镜像构建通过。
- 一个既有前后端 capability key 测试把 TypeScript 单引号和逗号误解析进键名。
- Windows 文件权限语义下，一个既有“不可读 Python 脚本”测试无法制造与 Unix 相同的不可读状态。

这些失败未发生在课题三修改的断言中，因此最终验收使用明确覆盖本课题变更的目标测试，并保留上述环境事实，不把全仓失败伪装成通过。

## 最终复核

以下结果均在分支 `feat/topic3-evaluation-cost-cache-20260828` 的最终工作树执行：

- Go 目标测试：`cmd/evalbench`、`cmd/evalctl`、`cmd/evalgate`、门禁、核心类型、五类模型包装器、评测/用量/缓存 Repository、评测 Service、用量 Handler、SQLite 全新安装与 v4 升级全部通过。
- 并发检查：Chat、Embedding 缓存、门禁在 `go test -race` 下通过。
- 静态检查：上述所有修改包的 `go vet` 通过；所有 Go 文件已执行 `gofmt`。
- 前端：`vue-tsc --build` 通过；国际化审计 11/11；测试 512/512；Vite 生产构建成功。构建只报告仓库既有的大 chunk 警告。
- CI 路径：`evalbench` 从当前代码生成 12 项质量指标，门禁比较 Recall、MRR、NDCG@10、ROUGE-L 共 4 项并通过。
- 失败路径：把 Recall 从 `0.82` 降到 `0.60` 后，命令以状态 1 退出并报告 `REGRESSION retrieval.recall ... change=-0.220000 allowed=0.030000`。
- 规格双审：首次规格审查发现 5 项缺口，均已修复并增加相应测试；最终复核结论见提交前差异检查记录。

## 选择性迁移复核（2026-09-12）

新增解析器语料、指标和运行器已通过以下检查：

- `go test -race ./internal/evaluation/...`
- `go test ./cmd/topic3-parser-corpus ./cmd/topic3-parser-benchmark ./internal/infrastructure/docparser/...`
- `go vet ./cmd/topic3-parser-corpus ./cmd/topic3-parser-benchmark ./internal/evaluation/... ./internal/infrastructure/docparser/...`

另外，Embedding 批量复用/并发合并、缓存隔离、普通 RAG/Wiki 稳定前缀和 DeepSeek cache usage 解析共 12 项聚焦测试在 `-race` 下通过。原始日志 SHA-256 为 `3c839a9f31abd5063e38ee3afc4191c0b04ba9b92d4097c73b4106152a4785ea`。

本轮也重新执行了 `go test ./...`。它以状态 1 结束，但没有迁移相关包失败；原始日志 SHA-256 为 `ff309a63c82ac3d2c9802125861f08b75a148d4cf4915c1a39b4c11a46c81a7e`。失败项来自仓库既有的 Windows 路径分隔符断言、DuckDB/MinGW 链接、SQLite 临时文件句柄、Docker `npipe`、Windows 文件权限语义、日志输出格式断言和前端 capability key 解析断言。选择性迁移从 `0d505c8b` 起没有修改这些失败涉及的文件。

机器可读摘要与 gzip 压缩的完整日志分别位于 `artifacts/migration-validation-summary.json` 和 `artifacts/migration-go-test-all.txt.gz`。Docker 恢复后已补做真实冷/热模型实验：缓存命中证据通过，但完整门禁因生成时延失败且价格未配置，整体记为 `partial`。它与确定性测试互为补充，不能互相替代。

最终代码双轴审查额外发现并修复三类 parser 问题：authoring ID 可构造目录逃逸、整体 deadline 在首个单元前触发时 ready 引擎会被汇总为 unavailable、DOCX ZIP/PNG 生成错误被吞掉。新增恶意 ID/路径逃逸和 deadline-before-first-attempt 测试后，`go test -race ./internal/evaluation/...` 通过，三个修改文件的 Serena Go 诊断为空。

同一轮复审还发现 estimated zero 会被普通数值门禁误判为成本通过。现在评测成本报告计算 `priced_call_count` 与 `cost_coverage_ratio`，`evalctl` 为每个成本指标写入显式 `metric_eligibility`，`evalgate` 对缺失或不合格的成本元数据均 fail closed。测试覆盖 Chat 的 input/output/cache-read/cache-write、Embedding provider 调用与全量应用缓存命中、Rerank，以及 legacy estimated-zero 报告拒绝路径；历史 portable JSON 仍可用于不涉及成本的诊断。

最终标准复核进一步要求拒绝矛盾的资格元数据。门禁现在校验覆盖率为有限的 `[0,1]` 数值，并要求 `eligible=true` 时覆盖率精确为 1；零覆盖、负数、大于 1 和 NaN 均有回归测试。最终聚焦验证还包含 Evaluation Service 的版本、模型快照与阶段耗时测试，日志 SHA-256 为 `755cda6b7794eca7b0fd98b4ad2a2f61c2c5ac959eacea1b5d7c2ae7789d9244`。

提交 `e0174424` 又完成 Linux 生产镜像构建与容器替换。运行中镜像 ID 为 `sha256:1fed47104b69ba89ef224add9d17cc6a0dbda1c4796ccdaa037d9f24b1e208dc`，二进制嵌入提交号 `e01744242e6f`，容器健康状态为 `healthy`，`/health` 返回 `{"status":"ok"}`。

Wiki 冷/暖真实运行均完成且异步队列归零。逐调用数据库事实证明 cache-read tokens 与读取比例上升；因为调用形状变化、暖跑仍产生 provider 计算且价格未知，验收状态保留为 `partial / not_comparable`。临时知识库、用户、个人租户和租户成员关系已清理。
