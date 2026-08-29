# 运行与验收手册

以下命令均在 WeKnora 仓库根目录执行。任何密钥只放进当前终端环境变量，不能写入命令历史、报告或 Git。

## 1. 准备服务

1. 按项目主 README 启动 WeKnora，并确认数据库迁移完成。
2. 在模型管理页至少配置一个 Embedding 和一个 Chat 模型；需要重排时再配置 Rerank。
3. 在模型编辑器的“价格”区域填写厂商账单价格。单位已固定显示为 USD/1M token 或 USD/1K 文档。
4. 准备租户 API Key 或登录令牌。

成功标志：模型列表可用，健康检查正常，新迁移中存在 `evaluation_runs`、`evaluation_cases`、`model_calls`、`embedding_cache`。

## 2. 一条命令运行固定评测

PowerShell：

```powershell
$env:WEKNORA_BASE_URL = "http://localhost:8080"
$env:WEKNORA_API_KEY = "<API Key>"
go run ./cmd/evalctl -dataset baseline-v1 -chat-model "<chat-id>" -embedding-model "<embedding-id>" -rerank-model "<rerank-id，可为空>" -summary-model "<summary-id>"
```

也可以额外固定已有知识库：

```powershell
go run ./cmd/evalctl -dataset baseline-v1 -chat-model "<chat-id>" -embedding-model "<embedding-id>" -rerank-model "<rerank-id，可为空>" -summary-model "<summary-id>" -knowledge-base "<kb-id>"
```

预期现象：终端先显示任务 ID，再显示完成进度，最终写入：

- `artifacts/evaluation-report.json`：供 CI 使用的扁平指标。
- `artifacts/evaluation-detail.json`：配置、四类汇总和逐题证据。
- `artifacts/evaluation-report.md`：数据集/配置/代码版本、四类汇总和逐题摘要的人类报告。

判断成功：任务状态为成功；`config.config_hash` 非空；质量、成本、耗时字段同时存在；数据库重启后仍能查询该任务。

## 3. 查询历史与模型用量

```text
GET /api/v1/evaluation/runs?limit=50&offset=0
GET /api/v1/evaluation?task_id=<task-id>
GET /api/v1/evaluation/cases?task_id=<task-id>&limit=100&offset=0
GET /api/v1/models/usage?model_id=<model-id>&start_time=2026-08-01T00:00:00Z&end_time=2026-08-31T23:59:59Z
```

`evalctl` 轮询第二个状态接口，任务完成后再自动分页取得第三个接口的逐题证据，因此题量增加时不会在每次轮询重复下载已有答案。模型管理页提供相同的模型和日期筛选；页面数字来自 `model_calls`，不是日志扫描结果。

## 4. 本地运行质量门禁

先用当前代码对审查过的固定证据重新计算指标，再执行严格门禁：

```powershell
go run ./cmd/evalbench -input testdata/evaluation/deterministic-cases.json -output artifacts/deterministic-report.json
go run ./cmd/evalgate -baseline testdata/evaluation/deterministic-baseline.json -current artifacts/deterministic-report.json -thresholds config/evaluation-gate-deterministic.json -json-out artifacts/gate.json
```

故意降低 Recall：

```powershell
go run ./cmd/evalgate -baseline testdata/evaluation/baseline.json -current testdata/evaluation/current-degraded.json -thresholds config/evaluation-gate.json
```

第二条命令应以非零状态退出，并输出 `REGRESSION retrieval.recall`、基线、当前值、变化量和允许阈值。这是验收“能指出具体退化指标”的确定性证据。

## 5. CI 配置

工作流 `Evaluation Regression` 支持 PR、工作日定时和手动运行。

- `Deterministic quality gate` 无需外部密钥，用当前提交的指标实现生成报告，再验证评测、模型观测、Embedding 缓存及门禁政策。
- `Live RAG quality gate` 在同仓库 PR 配置 `WEKNORA_EVAL_PR_BASE_URL` 时评测该 PR 的预览部署；定时/手动任务使用 `WEKNORA_EVAL_BASE_URL`。
- 真实评测还需 Secret `WEKNORA_EVAL_API_KEY`，以及变量 `WEKNORA_EVAL_BASELINE_CONFIG_HASH`、`WEKNORA_EVAL_CHAT_MODEL_ID`、`WEKNORA_EVAL_EMBEDDING_MODEL_ID`、`WEKNORA_EVAL_SUMMARY_MODEL_ID`；Rerank 启用时再设 `WEKNORA_EVAL_RERANK_MODEL_ID`，租户头可用 `WEKNORA_EVAL_TENANT_ID`。外部 fork 不读取 Secret，也不会运行真实评测。

要真正阻止合并，仓库管理员需在主分支保护规则中把检查：

```text
Evaluation Regression / Deterministic quality gate
```

设为必需检查。本分支不会擅自修改远端仓库设置。

若团队已经为 PR 部署当前分支预览服务，还应把：

```text
Evaluation Regression / Live RAG quality gate
```

设为必需检查。没有预览部署时，确定性检查能拦截指标实现和政策退化，但无法宣称替代一次真实 RAG 运行。

## 6. 审查并更新基线

基线不能由一次新运行自动覆盖。正确流程是：

1. 用显式固定的四类模型 ID 和相同 Pipeline 重复运行，排除外部故障和模型波动。
2. 检查逐题差异、模型调用、成本和耗时。
3. 由评审者确认变化合理。
4. 将 `evalctl` 原样生成的便携 JSON 以 `testdata/evaluation/live-baselines/<config_hash>.json` 提交并通过正常 PR 审查。
5. 把已经入库文件名中的哈希配置到 `WEKNORA_EVAL_BASELINE_CONFIG_HASH`。CI 不修改报告内哈希，并由 `evalgate` 再次校验两份报告条件一致。

如果配置哈希不同，先决定是恢复旧配置，还是建立一个新版本基线；不要直接比较。

## 7. 恢复与排错

- 数据集哈希错误：不要跳过校验，恢复清单对应文件或创建新版本清单。
- 任务在重启前中断：服务启动会把遗留运行标为失败；重新创建新任务，不复用原任务 ID。
- 成本为零：检查模型价格是否配置，以及提供商是否返回 token usage；调用次数仍应存在。
- 缓存未命中：核对租户、模型 ID、有效模型配置或向量维度是否变化。变化后不命中是正确行为。
- CI 外部服务超时：先区分基础设施故障和质量退化；只有 `REGRESSION <metric>` 才是阈值回归。
