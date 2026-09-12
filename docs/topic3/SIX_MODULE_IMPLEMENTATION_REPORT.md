# 课题三工程设计与实现报告

> 课题名称：评测、模型成本与缓存工程化
>
> 实现分支：`feat/topic3-evaluation-cost-cache-20260828`
>
> 代码基线：`721952cea39374b3e3df7dc71b421739c4232a71`
>
> 初始实现提交：`f1b1b3912ad1a3545c646c1fe35f5eac99615e84`
>
> 设计说明提交：`ab249f94`
>
> 选择性迁移与 Linux 验证提交：`c88047bf0285a1ded72f06ff038f3c1679b2ef2d`
>
> 计价证据提交：`4d4e654e066378c3f1ab434d5a855f12c5a43430`
>
> 报告更新日期：2026-09-12

## 1. 报告目的

本课题围绕 WeKnora RAG 链路实现可复现评测、模型调用与成本观测、Embedding 缓存、提示词缓存观测及质量回归门禁，并提供数据库迁移、前端查询、自动化测试和交付证据。

本报告把全部实现重新归纳为六个板块：

1. 可复现评测与质量门禁。
2. 模型调用观测与成本统计。
3. 模型用量查询与前端费用面板。
4. Embedding 缓存。
5. 提示词缓存优化与观测。
6. 工程保障与交付材料。

报告说明各模块的设计目标、架构边界、实现路径和验证证据。

## 2. 实现概览

```text
版本化数据集与运行配置
        ↓
执行 WeKnora RAG 评测
        ↓
持久化逐题结果、检索证据、模型用量、费用和耗时
        ↓
生成机器可读报告与审查报告
        ↓
按配置哈希匹配审查基线
        ↓
执行质量与成本回归门禁
```

与此同时，模型调用链路会复用已经计算过的 Embedding，并通过稳定提示词前缀提高厂商 Prompt Cache 可能命中的概率。这些优化不能绕开租户隔离，也不能改变模型真正看到的问答内容。

## 3. 总体技术框架

### 3.1 分层结构

本实现延续 WeKnora 原有的 Go 后端、Gin 路由、Service 应用服务、GORM Repository、Vue 3 前端和 Docker Compose 结构，没有单独引入另一套框架。

| 层次 | 职责 | 本课题中的实现 | 代表位置 |
| --- | --- | --- | --- |
| 入口层 | 提供 CLI、HTTP API 和 Vue 页面 | 评测执行、结果查询、门禁与模型用量管理 | `cmd/evalctl`、`cmd/evalbench`、`cmd/evalgate`、`internal/handler`、`frontend/src` |
| 编排层 | 协调应用服务流程 | 创建任务、临时知识库、索引、检索、生成、聚合与清理 | `internal/application/service/evaluation.go` |
| 领域逻辑层 | 实现独立业务规则 | 指标计算、配置指纹、成本公式与门禁比较 | `internal/application/service/metric`、`internal/evaluation/gate`、`internal/types` |
| 模型适配层 | 统一模型接口 | Chat、Embedding、Rerank、VLM、ASR 的观测与缓存包装 | `internal/models` |
| 数据层 | 持久化事实并提供租户级查询 | 评测运行、逐题结果、模型调用与向量缓存 | `internal/application/repository`、数据库迁移 |
| 展示层 | 查询并呈现模型用量 | 调用量、缓存命中、估算费用与价格配置 | `frontend/src/views/settings/ModelSettings.vue` |
| 工程层 | 提供构建、验证与交付能力 | GitHub Actions、Docker、运行手册与测试证据 | `.github/workflows`、`docker`、`docs/topic3` |

实现采用以下设计模式和数据约束：

- **Service Layer**：`EvaluationService` 承担评测编排，Handler 仅处理协议转换和鉴权上下文。
- **Repository Pattern**：Repository 封装持久化、租户约束、分页和聚合查询。
- **Decorator/Wrapper**：保持既有模型接口，通过组合加入调用观测与缓存能力。
- **Cache-Aside**：读取缓存，按未命中项调用模型，再写回结果。
- **Pure Function**：`EvaluationGate` 仅消费报告与阈值，保证确定性和可测试性。
- **Immutable Snapshot**：评测启动时固化数据集、模型、价格和 Pipeline 配置，历史结果不受后续配置变更影响。

### 3.2 主数据流

```text
baseline-v1 清单
   │ 校验路径、大小、SHA-256
   ▼
DatasetService
   │ 产生数据集快照
   ▼
EvaluationService
   ├── 创建临时知识库并同步索引
   ├── 对每道题执行 RAG Pipeline
   ├── 调用 Chat / Embedding / Rerank
   ├── 计算 12 项指标
   └── 清理临时知识库
   │
   ├──────────────► EvaluationRepository
   │                  保存运行、配置和逐题证据
   │
   └──────────────► ModelCallRepository
                      保存模型调用、Token、缓存、费用和耗时
                              │
                              ▼
            evalctl 生成 JSON / Detail JSON / Markdown
                              │
                              ▼
                  evalgate 与基线和阈值比较
                              │
                              ▼
                     退出码 0 或非 0
```

这条链路有一个重要边界：模型负责生成答案、向量或排序结果；WeKnora 负责组织输入、实际发起调用、执行检索流程、保存结果和决定任务生命周期。

## 4. 板块一：可复现评测与质量门禁

### 4.1 问题定义

原有评测能算出一些分数，但如果数据集文件、模型、Prompt、分块参数或检索参数已经变化，两次分数就不一定可以直接比较。只保存一个最终平均分，也无法知道是哪道题、哪个检索结果出了问题。

每份评测结果绑定以下实验身份：

```text
一份结果 = 固定数据集 + 固定模型 + 固定 Pipeline + 代码版本 + 逐题证据
```

### 4.2 版本化数据集和完整性校验

新增 [`dataset/manifests/baseline-v1.json`](../../dataset/manifests/baseline-v1.json)，为默认数据集声明：

- 稳定的数据集 ID 和版本号。
- 5 个 Parquet 文件的相对路径。
- 每个文件的字节数。
- 每个文件的 SHA-256。

SHA-256 用于验证数据集文件内容；任一文件发生字节变化时，清单校验失败。

[`DatasetService.GetDatasetSnapshot`](../../internal/application/service/dataset.go) 在加载时执行以下步骤：

1. 把空 ID 或 `default` 解析为 `baseline-v1`。
2. 拒绝包含路径穿越风险的数据集 ID。
3. 读取清单并逐个检查文件路径、大小和 SHA-256。
4. 加载问答、语料和相关性标注。
5. 按 QID 排序，避免文件遍历顺序改变题目执行顺序。
6. 返回包含版本、文件哈希和内容哈希的数据集快照。

评测任务创建时校验一次，后台真正开始执行时再次校验。第二次校验是为了避免任务排队期间文件被替换。

### 4.3 完整配置快照和条件哈希

评测开始时，[`EvaluationService`](../../internal/application/service/evaluation.go) 会冻结以下条件：

- 数据集 ID、版本、总哈希和各文件哈希。
- Chat、Embedding、Rerank、Summary 四种角色的模型 ID。
- 模型来源、模型名称、维度和去除凭据后的参数哈希。
- 当时使用的模型价格。
- 知识库来源。
- Chunk Size 和 Chunk Overlap。
- 检索阈值、Top K、重排配置、生成参数和 Prompt 等 Pipeline 参数。
- Pipeline 哈希、代码版本和整体 `config_hash`。

模型快照不会保存 API Key 和 App Secret。完整条件哈希也不包含代码版本，因为门禁的目的正是比较“相同实验条件下，不同代码版本”的表现；代码版本仍作为单独字段保留用于追溯。

### 4.4 持久化任务生命周期

评测运行不再只存在 Go 进程内存中，而是保存到：

- `evaluation_runs`：任务状态、配置、汇总指标、费用和耗时。
- `evaluation_cases`：每道题的答案、检索结果、重排结果、指标、错误和阶段耗时。

任务状态依次为：

```text
Pending → Running → Success
                  ↘ Failed
```

服务启动时，数据库中遗留的 `Pending` 或 `Running` 任务会被标记为失败并记录重启原因，避免状态与实际执行生命周期不一致。

Repository 的所有查询均带租户条件。即使知道另一个租户的任务 ID，也不能通过正常接口读取其结果。

### 4.5 真实 RAG 评测编排

评测没有另造一套简化问答逻辑，而是复用了 WeKnora 真实 RAG Pipeline：

1. 解析并二次校验数据集快照。
2. 根据指定模型创建评测专用临时知识库。
3. 把固定语料同步切块、生成向量并完成索引。
4. 对每道问题执行检索、可选重排和 Chat 生成。
5. 将检索 Chunk 映射回数据集中的稳定 passage ID。
6. 计算逐题检索指标和答案指标。
7. 保存逐题证据，更新完成进度。
8. 汇总质量、成本和耗时。
9. 无论成功或失败，都尝试清理临时知识与临时知识库。

离线评测最多使用 4 个并行 worker，并继续受模型自身并发控制约束。这样既能提高速度，也避免评测任务无限抢占交互请求需要的资源。

### 4.6 指标体系

线上评测与确定性评测共用 [`metric/registry.go`](../../internal/application/service/metric/registry.go) 中的统一注册表，避免两条路径各自维护一套名字或公式。

当前注册 12 项指标：

| 类别 | 指标 | 统计定义 |
| --- | --- | --- |
| 检索 | Precision | 检索结果中的相关 passage 比例 |
| 检索 | Recall | 标注相关 passage 的召回比例 |
| 检索 | NDCG@3、NDCG@10 | 截断位置内按相关性与排序折损计算的归一化增益 |
| 检索 | MRR | 首个相关 passage 排名倒数的均值 |
| 检索 | MAP | 各相关 passage 位置 Precision 的均值 |
| 回答 | BLEU-1、BLEU-2、BLEU-4 | 候选答案与参考答案的 n-gram 精确率 |
| 回答 | ROUGE-1、ROUGE-2、ROUGE-L | 候选答案对参考答案 n-gram 与最长公共子序列的覆盖率 |

这些指标用于固定数据集和固定计算口径下的回归比较，不能单独证明答案事实正确。

### 4.7 三个命令行入口

#### `evalctl`

[`cmd/evalctl`](../../cmd/evalctl/main.go) 是真实评测客户端。它负责：

- 读取环境变量中的服务地址和凭据。
- 发送创建评测请求。
- 轮询任务状态，但不在每次轮询下载答案正文。
- 完成后按每页最多 100 条分页获取逐题证据。
- 生成便携 JSON、完整 Detail JSON 和 Markdown 报告。

状态接口和逐题接口分离，是为了避免长任务在每次轮询时重复传输越来越多的历史答案。205 条用例测试验证了客户端会发出 3 次有界分页请求。

#### `evalbench`

[`cmd/evalbench`](../../cmd/evalbench/main.go) 使用仓库内固定证据，通过当前代码的指标实现重新计算报告。它不联网、不调用 DeepSeek、不产生模型费用，适合快速验证指标代码和 CI。

#### `evalgate`

[`cmd/evalgate`](../../cmd/evalgate/main.go) 读取基线报告、当前报告和阈值策略，再调用纯函数门禁。

### 4.8 质量门禁设计

[`internal/evaluation/gate`](../../internal/evaluation/gate/gate.go) 将“产生事实”和“判断是否放行”分开：

- 质量指标按“越高越好”比较。
- 成本和耗时按“越低越好”比较。
- 一次返回全部退化项，不在首个退化项处停止。
- 任一配置哈希为空或两份报告哈希不同，直接拒绝比较。
- 阈值策略为空、指标缺失或指标重复，同样直接失败。
- 全部通过时命令退出码为 `0`；发生退化或输入不可信时为非 `0`。

CI 以进程退出状态判定门禁结果：`0` 表示通过，非 `0` 表示退化或输入不满足比较合同。

### 4.9 实现结果

- 一次评测能被追溯到数据、模型、Pipeline 和代码版本。
- 重启后仍能查询历史运行。
- 可以从平均分下钻到具体题目、答案和 passage ID。
- 正常样例通过门禁；把 Recall 从 `0.82` 降到 `0.60` 后，门禁明确报告 `retrieval.recall` 退化并以状态 1 退出。

## 5. 板块二：模型调用观测与成本统计

### 5.1 问题定义

调用观测需要覆盖调用次数、模型耗时、失败率、输入/输出/缓存 Token、价格来源、估算费用和业务用途。统一的 `ModelCallRecord` 将每次逻辑模型调用持久化为可查询的数据事实。

### 5.2 统一调用记录

[`internal/types/model_call.go`](../../internal/types/model_call.go) 定义的记录主要包含：

- 租户、模型 ID、模型类型。
- `purpose`：本次调用的用途。
- `correlation_id`：把多次调用关联到同一个评测任务、请求或会话。
- 稳定提示词前缀指纹。
- 输入、输出、总 Token。
- Prompt Cache 读取、写入、未命中 Token。
- Embedding/Rerank 输入单位和条目数。
- Embedding 缓存命中、未命中条目数。
- 耗时、成功状态和错误类型。
- 本次调用的价格快照、估算费用和时间。

它不保存 Prompt、回答正文、原始文档和模型厂商的完整错误响应。

### 5.3 用包装器统一接入五类模型

本实现采用装饰器模式：保留原模型对象，在外层增加一个实现相同接口的包装器。

```text
业务代码
   ↓
观测/缓存包装器
   ├── 调用真实模型
   ├── 收集 usage、耗时和错误
   └── 写入 model_calls
   ↓
原 Chat / Embedding / Rerank / VLM / ASR 实现
```

对应实现包括：

- [`Chat 观测包装器`](../../internal/models/chat/observability_wrapper.go)
- [`Embedding 缓存与观测包装器`](../../internal/models/embedding/cache_wrapper.go)
- [`Rerank 观测包装器`](../../internal/models/rerank/observability_wrapper.go)
- [`VLM 观测包装器`](../../internal/models/vlm/observability_wrapper.go)
- [`ASR 观测包装器`](../../internal/models/asr/observability_wrapper.go)

这种设计的好处是模型厂商适配器不需要知道 GORM 或数据库表，也避免在每个业务调用点重复写计时代码。

### 5.4 流式调用记录约束

非流式 Chat 一次返回完整结果，记录一次比较直接。流式 Chat 会持续返回若干事件，最后一个事件或错误路径才可能包含最终 usage。

包装器因此需要保证：

- 正常流结束时写入一条汇总记录，各流片段不单独计费。
- 中途失败时记录失败状态。
- 已经收到的 usage 不因后续错误而丢失。
- 用户取消请求后，失败记录仍有短暂机会写入数据库。

为解决最后一点，`ModelCallPersistenceContext` 保留租户、用途和关联 ID 等 Context 值，但不继续继承原请求的取消信号，并设置 5 秒上限。这样不会因为浏览器关闭就完全丢失错误记录，也不会让观测写库无限挂起。

### 5.5 费用计算和价格快照

费用按照模型种类分别计算：

```text
Chat/VLM 费用
= 未缓存输入 Token × 输入单价
+ 输出 Token × 输出单价
+ 缓存读取 Token × 缓存读取单价
+ 缓存写入 Token × 缓存写入单价

Embedding 费用
= 输入估算单位 × Embedding 每百万单位价格

Rerank 费用
= 候选文档数 × 每千文档价格
```

价格在每次调用发生时复制进 `model_calls`。以后即使用户修改模型价格，历史费用也不会被新价格反向改写。

缓存读取、缓存写入和普通未命中输入是互斥计价类别，不会在普通输入价格上重复叠加。若厂商没有返回完整 usage，则可记录调用和耗时，但费用只能视为估算值。

### 5.6 五类模型的统计口径

| 模型类型 | 主要记录内容 | 当前边界 |
| --- | --- | --- |
| Chat | 输入、输出、缓存读写/未命中 Token | 依赖厂商返回的 usage 字段 |
| Embedding | 条目数、估算输入单位、缓存命中/未命中 | 当前用字符数近似 Token，用于估算而非账单对账 |
| Rerank | 候选文档数、耗时、成功状态 | 按每千文档价格估算 |
| VLM | 图片数量、估算 Token、耗时 | 接口未统一返回厂商 usage，因此标为 estimated |
| ASR | 音频字节数、耗时、结果状态 | 尚未实现按音频分钟计价 |

### 5.7 实现结果

模型调用以可聚合、可过滤且可关联评测运行的数据库记录保存。真实验收记录包含两次 Embedding 调用、一次 DeepSeek Chat 调用及最终汇总指标。

## 6. 板块三：模型用量查询与前端费用面板

### 6.1 后端查询 API

新增：

```text
GET /api/v1/models/usage
```

[`ModelHandler.GetModelUsage`](../../internal/handler/model.go) 接受：

- `model_id`：只看某个模型。
- `purpose`：只看某种用途。
- `start_time`、`end_time`：按 RFC3339 时间筛选。

Handler 负责解析和校验参数，Repository 负责带租户条件聚合 `model_calls`。结束时间早于开始时间会被拒绝，另一个租户的数据不会进入结果。

返回结果包含总计和 `by_model` 分组，例如：调用次数、成功次数、Token、缓存 Token、Embedding 命中/未命中、耗时和总费用。

### 6.2 前端数据流

```text
模型管理页面选择模型和日期
        ↓
frontend API 组装查询参数
        ↓
GET /api/v1/models/usage
        ↓
后端按当前租户聚合 model_calls
        ↓
页面显示调用次数、缓存读取、Embedding 命中率和估算费用
```

主要文件：

- [`frontend/src/api/model/index.ts`](../../frontend/src/api/model/index.ts)：TypeScript 类型和 API 调用。
- [`frontend/src/views/settings/ModelSettings.vue`](../../frontend/src/views/settings/ModelSettings.vue)：筛选器和汇总卡片。
- [`frontend/src/components/ModelEditorDialog.vue`](../../frontend/src/components/ModelEditorDialog.vue)：模型价格配置。
- [`frontend/src/utils/modelPricing.ts`](../../frontend/src/utils/modelPricing.ts)：价格对象的规范化与序列化。

### 6.3 页面展示内容

“设置 → 模型管理”增加了：

- 模型筛选。
- 日期范围筛选。
- 调用次数。
- Prompt Cache 读取 Token。
- Embedding 缓存命中率。
- 估算费用。
- 按模型拆分的统计。

模型编辑器根据模型类型展示相应的价格字段，统一注明 USD/1M token 或 USD/1K documents，避免把“每千”和“每百万”混在一起。

新增字段对旧客户端保持兼容：请求中没传价格字段时保留旧价格；显式发送空价格对象时才表示主动清空。这里区分了“字段缺失”和“用户清空”两种完全不同的意图。

### 6.4 实现结果

管理员不需要扫描日志或手工计算，就能在页面上按时间和模型观察调用、缓存和费用。页面数字来自数据库聚合，因此服务重启后仍然存在，也能与评测任务中的成本报告互相核对。

## 7. 板块四：Embedding 缓存

### 7.1 缓存正确性约束

Embedding 结果取决于输入文本、模型、模型配置和输出维度。缓存键缺少任一影响结果或租户隔离的维度，都会造成向量误复用和检索质量偏差。

### 7.2 五维缓存键

缓存键定义为：

```text
(tenant_id, model_id, dimensions, model_config_fingerprint, SHA256(text))
```

每一维解决一个风险：

| 维度 | 作用 |
| --- | --- |
| `tenant_id` | 防止不同租户互相读取派生数据 |
| `model_id` | 区分用户配置的不同模型对象 |
| `dimensions` | 防止 768 维、1024 维等不同长度向量混用 |
| `model_config_fingerprint` | 模型 ID 被复用但提供商、地址、模型名或有效参数变化时自动失效 |
| `SHA256(text)` | 精确标识输入内容，同时避免数据库保存原文 |

模型配置指纹会排除 API Key、App Secret 和价格。轮换密钥或修改价格不会改变向量含义，因此不应浪费已有缓存；模型来源、模型名、地址、维度等有效配置变化则必须失效。

### 7.3 文本粒度与预处理取舍

缓存键作用于送入 Embedder 的每个字符串，覆盖 Query、Chunk 和批量 Chunk。文档局部修改后，内容未变的 Chunk 仍可复用。

缓存对实际输入字符串的 UTF-8 字节计算 SHA-256，不执行空格折叠、大小写转换、Unicode 改写或语义近似匹配；此类规范化可能改变代码、表格、专有名词或模型输入语义。

因此当前策略是：

- 字节完全相同：允许复用。
- 只要文本字节不同：视为不同输入。

这是偏向正确性的保守取舍。将来若增加文本规范化，必须把规范化算法及版本也纳入缓存键。

### 7.4 Cache-Aside 流程

单条请求的流程是：

```text
构造五维键
   ↓
查询 embedding_cache
   ├── 命中：返回向量并记录 hit
   └── 未命中：获取该键的进程锁
                 ↓
              再查一次数据库
                 ├── 已被其他请求写入：直接返回
                 └── 仍未命中：调用真实 Embedding 模型
                                   ↓
                                回填缓存并返回
```

请求取得锁后再次查询缓存，以复用等待期间由其他请求写入的结果，避免重复调用 Provider。

### 7.5 批量部分命中与顺序恢复

批量输入可能是：

```text
[A, B, C, B]
```

其中 A、B 已缓存而 C 未缓存。包装器会：

1. 为结果创建与输入等长的位置数组。
2. 把已命中向量放回原位置。
3. 对未命中文本去重，但保留第一次出现的稳定顺序。
4. 只把仍缺失的文本组成一个批次发给模型。
5. 把返回向量填回所有对应位置。

因此既保留模型的批处理效率，也保证输出第 `i` 个向量仍对应输入第 `i` 个文本。

### 7.6 并发控制

实现使用进程级按键锁表。ModelService 按请求创建轻量包装器，因此锁必须跨包装器共享，才能合并不同请求的重复计算。

批量请求会先按稳定顺序排序所有需要加锁的键，再依次加锁，并在退出时反向解锁。稳定顺序可以降低交叉批次死锁风险。

并发正确性有两层保障：

- 进程内按键锁：尽量让同一进程中的重复未命中只发起一次模型调用。
- 数据库复合唯一约束和幂等写入：即使有多个应用进程同时计算，也不会插入重复缓存行。

数据库唯一约束不能阻止两个进程都花钱调用模型，但能保证持久化结果一致。若将来需要跨进程抑制重复计算，可以再引入 Redis 分布式锁或跨进程 singleflight。

### 7.7 故障降级

- 缓存读取失败：记录警告，继续调用真实模型。
- 缓存写入失败：记录警告，但仍把已经得到的有效向量返回给业务。
- 命中计数更新失败：不能把正确向量变成业务错误。
- Provider 返回向量数量与缺失输入数不一致：直接报错，避免错位填充。

缓存故障采用降级策略，核心请求正确性不依赖缓存可用性。

### 7.8 缓存失效与当前限制

修改或删除数据库中的模型配置时，会主动清除该模型的派生缓存；有效配置指纹变化也会形成自然失效。

当前尚未实现：

- 自动容量上限。
- TTL 过期策略。
- 后台清理任务。
- 跨进程重复调用合并。

因此生产使用时仍要监控 `embedding_cache` 表容量。

### 7.9 实现结果

测试证明：

- `[alpha, beta]` 第一次触发一次 Provider 批调用。
- `[beta, alpha]` 第二次不再调用 Provider，命中 2 项且顺序正确。
- 8 个并发相同单条请求只触发 1 次 Provider 调用。
- 两个重叠并发批次也只触发 1 次批调用。
- 更换租户、模型、配置指纹或维度时不会错误命中。

## 8. 板块五：提示词缓存优化与观测

### 8.1 Prompt Cache 机制

Prompt Cache 或 Context Cache 复用已处理的相同请求前缀，以降低重复计算和相应费用。

其核心条件通常是：从第一个字符开始，前缀要尽可能稳定。固定规则前面如果出现当前时间、用户问题或检索资料，后续所有固定内容也很难被当作相同前缀复用。

### 8.2 稳定部分和动态部分

普通 RAG 系统 Prompt 中通常同时包含：

```text
稳定部分：角色、回答规则、引用规范、输出格式
动态部分：当前问题、检索上下文、当前时间、会话记忆
```

[`prepareMessagesWithHistory`](../../internal/application/service/chat_pipeline/common.go) 保持真正发送给模型的完整 Prompt 内容不变，但额外识别第一个动态占位符：

```text
{{contexts}}、{{query}}、{{current_time}}
```

第一个动态占位符之前的模板内容被定义为稳定前缀；检索上下文、问题、记忆和历史消息仍按原来的方式进入完整请求。

### 8.3 观测边界

实现保持模型请求内容不变，识别稳定前缀边界并计算不可逆指纹；回答规则和检索上下文均未删改。

```text
稳定前缀内容
   ↓ JSON 序列化稳定结构
SHA-256
   ↓
PromptPrefixFingerprint
```

指纹相同表示两个请求具有相同的稳定结构，但数据库无法从指纹还原 Prompt 原文。

[`PromptPrefixFingerprint`](../../internal/models/chat/prompt_cache.go) 同时把稳定系统消息和工具定义纳入计算，因为工具 Schema 改变也可能导致厂商缓存前缀发生变化。

### 8.4 普通 RAG 与 Wiki 路径

- 普通 RAG：稳定系统规则位于检索上下文和问题之前；本课题补充稳定前缀指纹和用量观测。
- Wiki：继续复用项目已有的稳定前缀和缓存元数据机制，并通过测试保证用途和指纹继续传递到模型层。

对于 DeepSeek，服务端会尽力复用相同前缀，并通过命中/未命中 Token 字段返回观测结果。本实现负责保持前缀稳定和统一记录字段，但不能保证厂商一定命中。

### 8.5 跨厂商 usage 归一化

不同厂商对缓存 Token 的命名不同，例如缓存读取、缓存创建、命中和未命中可能位于不同字段。统一的 Chat 观测包装器把厂商返回值映射到：

- `cache_read_tokens`
- `cache_write_tokens`
- `cache_miss_tokens`

这样上层页面和成本公式不需要理解每家厂商的原始协议。

### 8.6 有效性判据

缓存有效性由以下证据共同判定：

1. 两次请求的稳定前缀指纹相同。
2. 模型、工具、关键 Header 和稳定前缀字节一致。
3. 厂商响应中出现缓存读取或命中 Token。
4. 数据库 `model_calls` 能看到相应字段变化。
5. 质量指标没有超过允许的退化范围。

自动化测试证明不同问题和上下文下的稳定前缀指纹一致，且指纹输入不包含问题和检索文本。真实冷暖运行进一步记录 Chat cache-read tokens 从 0 增至 1024；暖运行仍有 113 个 cache-miss tokens，因此保持 `not_comparable`。

### 8.7 实现结果

- 普通 RAG 的稳定规则和动态数据边界更加明确。
- 模型实际接收的问答语义保持不变。
- 可以按指纹观察某类请求前缀是否稳定。
- 能把 DeepSeek、OpenAI-compatible 和 Anthropic 风格的缓存 usage 归一到统一统计口径。
- 指纹与 usage 数据支持定位 Prompt Cache 失效原因。

## 9. 板块六：工程保障与交付材料

### 9.1 数据库迁移

新增 PostgreSQL 和 SQLite 两组版本化迁移：

- [`migrations/versioned/000090_evaluation_observability.up.sql`](../../migrations/versioned/000090_evaluation_observability.up.sql)
- [`migrations/versioned/000090_evaluation_observability.down.sql`](../../migrations/versioned/000090_evaluation_observability.down.sql)
- [`migrations/sqlite/000013_evaluation_observability.up.sql`](../../migrations/sqlite/000013_evaluation_observability.up.sql)
- [`migrations/sqlite/000013_evaluation_observability.down.sql`](../../migrations/sqlite/000013_evaluation_observability.down.sql)

迁移创建：

1. `evaluation_runs`
2. `evaluation_cases`
3. `model_calls`
4. `embedding_cache`

`up` 用于升级，`down` 用于回滚。表中建立了租户查询索引、任务查询索引和缓存复合唯一约束。SQLite 测试同时覆盖全新安装和从旧版本升级并保留数据。

### 9.2 测试体系

测试覆盖以下层次：

| 层次 | 验证内容 |
| --- | --- |
| 单元测试 | 指标、成本公式、配置指纹、缓存键、门禁边界 |
| Repository 测试 | 落库、聚合、过滤、租户隔离、重启恢复 |
| Service/Handler 测试 | 评测流程、状态分页、API 参数和兼容性 |
| 并发测试 | 单条/批量 Embedding 合并、流式 Chat 只记一次 |
| 迁移测试 | 四张表、SQLite 新装和旧版升级 |
| 前端测试 | 类型、价格处理、国际化、组件逻辑和生产构建 |
| 竞态检查 | Go `-race` 检测实际执行路径中的不安全并发读写 |
| 静态检查 | `go vet`、`gofmt`、TypeScript 类型检查 |
| 确定性验收 | 当前代码生成 12 项指标，门禁检查 4 项并验证故意退化路径 |

最终目标测试、Chat/Embedding/门禁竞态检查、相关包 `go vet`、前端 512/512 测试、11/11 国际化检查和 Vite 生产构建均通过。

Windows 原生环境仍存在 SQLite 文件句柄、Docker `npipe` 和 MinGW/DuckDB 链接限制；最终交付环境改用 Linux amd64、Go 1.26，并已完成全仓测试。

### 9.3 GitHub Actions

[`evaluation-regression.yml`](../../.github/workflows/evaluation-regression.yml) 提供两个 Job。

#### Deterministic quality gate

- 在 Pull Request、定时和手动触发时运行。
- 不需要外部密钥。
- 执行评测、观测、缓存、API、迁移和前端价格接口测试。
- 用当前提交编译出的指标代码运行 `evalbench`。
- 使用审查过的阈值运行 `evalgate`。
- 无论成功失败都上传门禁报告 Artifact。

#### Live RAG quality gate

- 只有部署地址、模型 ID、基线哈希和 Secret 配置齐全时运行。
- 同仓库 PR 可针对当前分支预览环境；定时/手动任务使用固定部署环境。
- 外部 Fork 不获得 Secret，也不会执行真实评测。
- 对版本化数据集运行真实 RAG，再与对应配置哈希的人工审查基线比较。

Workflow 文件只定义自动检查。要真正禁止不合格代码合并，仓库管理员仍需把 `Evaluation Regression / Deterministic quality gate` 设置为受保护分支的必需检查。

### 9.4 Docker 构建和代码版本溯源

演示当前分支前必须重新构建 Docker 镜像，避免运行旧 `v0.7.2` 镜像。

[`docker/Dockerfile.app`](../../docker/Dockerfile.app) 接收 `COMMIT_ID_ARG`，构建命令为：

```powershell
$commit = git rev-parse --short HEAD
docker compose build --build-arg COMMIT_ID_ARG=$commit app
docker compose up -d --no-build app
```

真实验收曾发现报告中的 `code_version=unknown`。Docker 构建上下文不包含 `.git`，运行时无法直接读取提交记录。修复后优先使用构建时注入的 Commit ID，并以 Go build info 作为本地构建回退。初次修复镜像验证提交为 `0d505c8b`；最终验证镜像嵌入提交 `c88047bf0285a1ded72f06ff038f3c1679b2ef2d`，包含 `baseline-v1` 清单且健康检查返回 HTTP 200。

真实部署验证补充覆盖了构建上下文和版本注入问题。

### 9.5 安全和隐私

- 密钥只通过环境变量或 CI Secret 传入，不写入源码和报告。
- `model_calls` 不保存 Prompt、回答、原始资料或厂商错误正文。
- `embedding_cache` 不保存原始文本。
- `evaluation_cases` 为复算质量而保存固定测试集答案和 passage ID，因此必须继续受租户权限保护。
- 数据集 ID 拒绝路径穿越。
- 模型用量、评测和缓存查询均以租户为第一隔离条件。
- 外部 Fork 无法读取真实评测 Secret。

哈希只能降低原文直接暴露和检测内容变化，不能替代权限校验。

### 9.6 交付文档

课题三文档集中放在 [`docs/topic3`](./README.md)：

| 文档 | 用途 |
| --- | --- |
| `README.md` | 交付总入口和最短验收路径 |
| `SPEC.md` | 范围、设计约束和验收标准 |
| `ARCHITECTURE.md` | 模块边界、数据流和数据库结构 |
| `RUNBOOK.md` | 启动、评测、门禁、CI 和排错命令 |
| `EXPERIMENT_REPORT.md` | 确定性实验、缓存证据和真实实验边界 |
| `TEST_REPORT.md` | 通过项与已知环境失败 |
| `IMPLEMENTATION_CHALLENGES_AND_REFERENCES.md` | 15 组重难点、外部一手参考和项目取舍 |
| `LIMITATIONS.md` | 尚未实现或需要管理员完成的事项 |
| `submission.yaml` | 机器可读的分支、命令和文档索引 |
| `demo-20260903/ACCEPTANCE_REPORT.md` | 当前分支真实 RAG 验收记录 |

## 10. 模块协作关系

```text
板块一：定义怎样测、怎样判断退化
   ↑                              │
   │                              ↓
板块五：观察 Prompt Cache     板块二：记录每次模型调用
   ↑                              │
   │                              ↓
板块四：减少重复 Embedding    板块三：把用量与费用展示给人
   \                              /
    └──────── 板块六 ───────────┘
       迁移、测试、容器、CI、文档和安全
```

一次真实评测会同时经过多个板块。例如：

1. 板块一固定数据集和配置，创建任务。
2. 评测建库时调用 Embedding，板块四先查缓存。
3. 所有模型调用由板块二记录用量、费用、耗时和关联 ID。
4. Chat Prompt 由板块五提取稳定前缀指纹，并记录厂商缓存 Token。
5. 板块一保存逐题证据并生成报告。
6. 板块三可以查询这次运行对应的模型用量。
7. 板块六确保数据库表存在、测试通过、镜像版本可追溯，CI 再执行门禁。

## 11. 真实验收结果

2026-09-03 已在当前分支重建的 Docker 服务上完成一次真实、单题 RAG 评测：

- 任务 ID：`evaluation_10000_1788438818252_98b36181_baselinev1`
- 数据集：`baseline-v1`
- 状态：成功，`1/1`
- 总耗时：18.9 秒
- 索引耗时：5.051 秒
- 单题平均耗时：13.783 秒
- 检索、生成链路均真实执行
- Recall、Precision、MRR、MAP、NDCG@3、NDCG@10：均为 1.0
- ROUGE-L：0.25
- 模型调用：3 次
- Embedding 冷缓存未命中：5 项
- DeepSeek Chat：输入 1,137 Token，输出 609 Token
- 临时知识库在结束时完成软删除

该次运行记录 `$0` 且 `cost_estimated=true`，原因是当时未配置非零价格；该字段不能用于推断厂商账单金额。

完整证据见 [`demo-20260903/ACCEPTANCE_REPORT.md`](demo-20260903/ACCEPTANCE_REPORT.md)。这次只有一道题且只完成冷缓存运行，因此可作为人工验收证据，不能单独升级为长期 CI 正式基线，也不能证明热缓存效果。

## 12. 关键设计取舍

### 12.1 配置哈希与代码版本分离

配置哈希仅标识实验条件，代码版本单独记录实现来源。该设计允许不同代码提交在实验条件一致时进入回归比较。

### 12.2 缓存采用精确文本匹配

语义相似不能作为模型输入等价条件。当前缓存仅复用字节完全相同的文本，以保持检索行为可验证。

### 12.3 缓存持久化到数据库

数据库缓存可以跨请求和进程重启保留，也便于租户隔离和审计；代价是每次需要访问数据库，且容量需要运维管理。

### 12.4 观测链路故障策略

模型已成功返回结果时，观测写入失败记录警告并保留业务响应。核心评测事实、数据结构约束和测试仍保证统计路径可审计。

### 12.5 确定性门禁与真实 RAG 门禁分离

确定性门禁用于每个 PR，提供低成本且稳定的回归信号。真实模型评测覆盖部署与供应商链路，并单独管理费用、延迟和随机波动；两类结果使用独立的证据状态。

## 13. 已知限制和后续工作

1. 当前数据集为最小规模样例，质量指标只支持工程回归验证，不支持统计显著性结论。
2. 真实 RAG 冷暖运行已完成，但暖运行仍包含 113 个 Chat cache-miss tokens，严格结论为 `not_comparable`。
3. DeepSeek chat 已完成一次性 USD 计价验证；本地 Ollama Embedding 在 cache miss 时仍缺少可核验的供应商费率。
4. VLM usage 以估算为主，ASR 尚未实现按分钟计价。
5. Embedding 缓存没有 TTL、容量上限和后台清理任务。
6. 进程内并发合并不能抑制跨应用实例的重复 Provider 调用。
7. Prompt Cache 必须结合供应商 usage 字段判定实际命中，稳定指纹仅用于验证请求前缀一致性。
8. GitHub 分支保护和 required check 需要仓库管理员配置。
9. Windows 原生测试受 SQLite 文件句柄、Docker `npipe` 与 MinGW/DuckDB 链接影响；Linux amd64、Go 1.26 全仓测试已通过。
10. 可选解析器横评已实现固定 8×7 矩阵；当前环境 `1/8` 引擎 ready，结果状态为 `partial`。

## 14. 完整验收建议

### 14.1 不调用模型的确定性验收

```powershell
go run ./cmd/evalbench -input testdata/evaluation/deterministic-cases.json -output artifacts/deterministic-report.json
go run ./cmd/evalgate -baseline testdata/evaluation/deterministic-baseline.json -current artifacts/deterministic-report.json -thresholds config/evaluation-gate-deterministic.json -json-out artifacts/gate.json
```

成功标志：门禁输出通过，退出码为 0。

再运行故意退化样例：

```powershell
go run ./cmd/evalgate -baseline testdata/evaluation/baseline.json -current testdata/evaluation/current-degraded.json -thresholds config/evaluation-gate.json
```

成功标志：命令以非零状态退出，并明确指出 `retrieval.recall`。

### 14.2 真实 RAG 验收

先从当前分支重建镜像，再在当前 PowerShell 终端设置服务地址和临时凭据：

```powershell
$commit = git rev-parse --short HEAD
docker compose build --build-arg COMMIT_ID_ARG=$commit app
docker compose up -d --no-build app
```

随后按照 [`RUNBOOK.md`](RUNBOOK.md) 使用 `evalctl`。成功标志包括：

- 进度从 `0/N` 到 `N/N`。
- 生成便携报告、详细报告和 Markdown 报告。
- `config_hash` 非空。
- 质量、成本、耗时和逐题证据同时存在。
- 报告包含可解析的代码版本，且值不为 `unknown`。
- 服务重启后仍能查询任务。

### 14.3 缓存验收

固定相同租户、模型、配置、维度和数据集连续运行两次，比较：

- `cache_hit_items`
- `cache_miss_items`
- Embedding Provider 调用次数
- 质量指标
- 模型调用耗时

预期现象是第二次命中增加、真实 Embedding 计算减少，同时质量没有超过允许退化范围。

## 15. 初始实现结果

初始实现完成六项工程能力：

- 版本化数据集、配置快照和逐题评测证据。
- 模型调用观测、价格快照与成本聚合。
- 模型用量查询 API 和前端面板。
- 具备租户、模型、配置和维度隔离的 Chunk 级 Embedding 缓存。
- 稳定前缀指纹与供应商 cache usage 归一化。
- 数据库迁移、测试、Docker、CI、安全约束和交付文档。

每次回归评测均可关联固定条件、逐题证据、模型成本、缓存观测和门禁结果。

## 16. 选择性迁移与最终验证

在保持本分支四表评测与缓存架构不变的前提下，第二套实现中的解析器基准被选择性迁入。迁移未使用 cherry-pick 或整分支合并，且未引入第二套六表调用记录模型或 runtime factory；新增范围包含七文档语料及独立审核、五项解析指标、固定 8×7 运行矩阵、实际 fallback 记录和报告 CLI。

缓存主线没有另起一套实现。现有 Embedding cache、普通 RAG 稳定前缀、Wiki 缓存布局和供应商 usage 归一化继续作为唯一产品路径，并用 12 项竞态测试重新验证。Docker 恢复后，当前提交的冷/热实跑进一步证明 Embedding 命中从 0/5 升至 5/5、Chat cache-read tokens 从 0 升至 1024；但暖跑仍有 113 个 provider cache-miss tokens，严格结论为 `not_comparable`。价格缺失和时延门禁失败仍按 `partial` 如实披露，原始 gate 的 estimated-zero 成本检查不作为有效通过。

Wiki 切入口也完成两轮真实运行：`wiki_*` cache-read tokens 从 2,560 增至 6,784。由于调用形状随 Wiki 状态演进、暖跑仍有新计算且价格未知，该证据只证明真实缓存使用，不声称端到端成本或时延改善。完整映射见 [`REQUIREMENT_TRACEABILITY.md`](REQUIREMENT_TRACEABILITY.md)。

解析器本机实测为 `partial`、`1/8` ready；固定 56 单元全部保留，未配置引擎明确记录 unavailable/unsupported。完整选择、证据和复现方式见 [`MIGRATION_REPORT.md`](MIGRATION_REPORT.md)。

DeepSeek chat 的一次性货币层验证在官方谷价时段内完成。新增 3 条调用均保存非零 USD 金额和价格快照，时间窗 Summary 为 USD 0.003405424；模型配置随后显式恢复为空。该证据不回填历史调用，也不用于推算原冷暖运行的金额差。本地 Ollama Embedding 在 cache miss 时仍保持费用 unknown。

最终候选代码已通过 Linux amd64、Go 1.26 全仓测试、全仓 `go vet`、关键路径 `-race`、确定性质量门禁、生产镜像健康检查及双轴代码审查。需求映射和剩余限制分别见 [`REQUIREMENT_TRACEABILITY.md`](REQUIREMENT_TRACEABILITY.md) 与 [`LIMITATIONS.md`](LIMITATIONS.md)。
