# 课题三：实现重难点与官方参考资料

> 面向初学者的阅读说明：本文只引用厂商官方文档、标准、原始论文或项目官方源代码。链接内容说明“行业中已有的定义或机制”；“映射到本项目”说明 WeKnora 如何落地；“工程推导”是本项目为了满足课题目标做出的设计判断，**不是来源原话**。
>
> 核对日期：2026-08-31。模型能力、计价和厂商返回字段都可能变化，正式验收前应再次查看对应官方页面。

## 先看结论

课题三的核心不是“多算几个分数”，而是建立一条能追溯的证据链：

```text
固定数据集 + 固定有效配置 + 代码版本
                 ↓
             执行一次评测
                 ↓
  检索/生成质量 + 时延 + Token/缓存/费用
                 ↓
       与同配置基线进行回归比较
                 ↓
          CI 留存报告并决定通过/失败
```

本地实现已经覆盖这条主链，但有几个必须保留的边界：哈希用于识别内容，不自动阻止有权限的人替换内容；供应商 Prompt Cache 是“观察并归一化”，不是 WeKnora 自己伪造命中；Embedding 缓存的并发合并只在单个进程内生效；费用是基于调用时价格快照的估算，不等于厂商账单；MAP、BLEU、ROUGE 的本地实现属于明确的“项目口径”，不能无条件与论文或外部榜单横向比较。

## 1. 可复现评测：数据集版本、哈希和配置快照

### 陌生名词

- **可复现（reproducible）**：别人拿到相同输入和配置，能够重新执行，并知道自己比较的是不是同一实验条件。生成模型仍可能有随机性，因此“可复现”不保证逐字相同。
- **SHA-256**：把任意内容压缩成固定长度“指纹”的算法。内容变化时，指纹通常也会变化。
- **快照（snapshot）**：在运行开始时把真正生效的配置冻结记录下来，而不是以后再读取可能已经修改的配置。

### 一手来源说了什么

1. [NIST FIPS 180-4：Secure Hash Standard](https://csrc.nist.gov/pubs/fips/180-4/upd1/final) 定义了 SHA-256 等安全哈希算法。哈希可用于检测消息内容是否发生变化，但它本身不是权限控制或数字签名。
2. [MLflow Tracking 官方文档](https://mlflow.org/docs/latest/ml/tracking/) 把一次实验运行描述为参数、指标、代码版本和输出制品的组合。这不是 WeKnora 的依赖，而是“实验必须同时记录输入条件和结果”的官方项目实践参考。
3. [OpenAI API 官方说明](https://platform.openai.com/docs/api-reference/introduction) 提醒模型输出具有非确定性，并建议使用固定模型版本和评测来获得更稳定的应用行为。

### 如何映射到本地实现

- [`dataset/manifests/baseline-v1.json`](../../dataset/manifests/baseline-v1.json) 为每个评测数据文件保存文件名、字节数和 SHA-256，而不是只靠一个容易重复的目录名。
- [`internal/application/service/dataset.go`](../../internal/application/service/dataset.go) 会限制数据集标识的格式、校验路径没有逃出数据目录，并重新计算大小和 SHA-256；manifest 内容还会形成一个规范化总哈希。
- [`internal/types/evaluation.go`](../../internal/types/evaluation.go) 的 `EvaluationConfigSnapshot` 记录数据集 ID/版本/哈希、模型 ID 与脱敏后的模型配置快照、知识库、分块参数、流水线哈希、配置哈希和代码版本。
- [`internal/application/service/evaluation.go`](../../internal/application/service/evaluation.go) 在创建评测时冻结有效配置；真正执行前再次验证数据集，并把运行、用例和结果持久化。
- `ConfigHash` 的计算有意排除 `CodeVersion`：这样才能在“配置相同、代码改变”的条件下做代码回归比较；代码版本仍被单独保存用于追踪。

### 本项目自己的工程推导

- “清单文件 + 文件哈希 + 有效配置快照 + 代码版本”这一组合是本项目的可复现协议，不是 NIST 或 MLflow 规定的唯一实现方式。
- 模型密钥不参与可公开比较的配置内容；模型端点、模型名和影响行为的参数应参与，而价格变化另由调用时价格快照记录。这是安全性、可比性和审计需求之间的取舍。
- 在执行前二次验证数据集，是为避免“创建任务后、真正运行前文件被替换”的时间窗口。

### 局限

- SHA-256 能识别内容，却不能证明“谁”提供了内容；有写权限的人仍可同时替换数据和 manifest。版本控制、代码评审和分支保护仍然必要。
- 外部模型可能静默更新；即使请求参数和模型别名相同，也不保证输出逐字一致。评测应比较统计指标和允许范围，而不是只比较答案文本。
- 当前配置哈希代表本项目选择纳入的字段；以后新增影响行为的配置时，必须同步加入快照，否则会出现“哈希相同、实际条件不同”。

## 2. RAG 的检索与生成指标

### 陌生名词和权威定义

RAG（Retrieval-Augmented Generation，检索增强生成）先从知识库找材料，再让模型依据材料回答。因此至少要分别观察“找得对不对”和“答得像不像参考答案”。

| 指标 | 直观问题 | 定义摘要与一手来源 |
|---|---|---|
| Precision（准确率） | 找回的这些结果里，有多少是相关的？ | `相关且被取回数 / 被取回总数`。[NIST TREC 评测说明](https://trec.nist.gov/pubs/trec30/papers/Overview-2021.pdf) |
| Recall（召回率） | 所有应该找到的结果里，实际找到了多少？ | `相关且被取回数 / 全部相关结果数`。[NIST TREC 评测说明](https://trec.nist.gov/pubs/trec30/papers/Overview-2021.pdf) |
| RR / MRR | 第一个正确结果排得有多靠前？ | 单题 RR 是第一个正确答案排名的倒数，没有正确答案为 0；MRR 是多题平均。[NIST TREC QA 数据说明](https://trec.nist.gov/data/qa.html) |
| AP / MAP | 每次出现相关结果时，前缀质量整体如何？ | 标准 AP 通常把每个相关位置上的 Precision 求和后除以该查询的相关文档总数，MAP 再对查询取平均。[NIST 官方 `trec_eval`](https://github.com/usnistgov/trec_eval) 是可核对的参考实现。 |
| DCG / NDCG | 高相关结果是否尽量排在前面？ | DCG 对靠后的结果做对数折损；NDCG 再除以理想排序的 DCG。[原始 NDCG 论文](https://doi.org/10.1145/582415.582418) |
| BLEU | 生成文本的短语与参考答案重合多少？ | 对多个 n-gram 精确率取几何平均，并加入长度惩罚；原论文强调语料级机器翻译评测。[BLEU 原始论文](https://aclanthology.org/P02-1040/) |
| ROUGE | 参考摘要中的词或序列被覆盖多少？ | ROUGE 是一组基于 n-gram、词序列和最长公共子序列的自动摘要指标。[ROUGE 原始论文](https://aclanthology.org/W04-1013/) |

### 如何映射到本地实现

- [`internal/application/service/metric/registry.go`](../../internal/application/service/metric/registry.go) 统一登记评测可使用的指标，避免 API、执行器和报告各自维护一份名称。
- 检索指标实现位于 [`internal/application/service/metric`](../../internal/application/service/metric)：Precision、Recall、MRR、MAP、NDCG@3 和 NDCG@10 均从排序后的检索结果和标注相关项计算。
- 生成指标同样位于该目录：中文先分词，再计算 BLEU、ROUGE-1、ROUGE-2 和 ROUGE-L。
- 报告中的检索、生成、时延和成本指标分组展示，避免把“检索没找到”和“模型没利用找到的材料”混成一个问题。

### 本项目自己的工程推导与口径差异

- 本地 NDCG 使用二元相关性（相关为 1，不相关为 0），不支持论文中的多级相关性评分。这适合当前数据集，但信息量较少。
- 本地 MAP 实现把命中位置的 Precision 之和除以 `hitCount`，而常见 TREC AP 口径除以“标注的相关文档总数”。当有相关文档完全未被找回时，本地数值可能偏高；它应标为项目口径，后续若修改必须建立新基线。
- 本地 BLEU 是经过平滑的句子级变体，并对中文分词；它不是原论文强调的语料级 BLEU，也不能直接与其他分词器或 SacreBLEU 榜单比较。
- 本地 ROUGE 返回 F 分数，并使用项目自己的去重/集合式重合处理；原始 ROUGE 家族包含不同统计口径。因此报告必须带上实现版本，不能只写“ROUGE”。

### 局限

- BLEU/ROUGE 主要衡量表面重合，不直接证明事实正确、引用可靠或回答有帮助。
- Precision/Recall 等需要可信的相关性标注；标注遗漏会使“正确检索”被误判为错误。
- 指标只在相同数据集、切分、候选集合、`k` 值、分词与实现版本下可直接比较。

## 3. 评测回归门禁、基线不可变与 GitHub Actions

### 陌生名词

- **基线（baseline）**：一组经过确认、用来比较的历史结果。
- **回归（regression）**：改代码后，已有能力意外变差。
- **门禁（gate）**：在合并前自动检查；超过允许退化阈值就失败。
- **制品（artifact）**：工作流产生并保存的报告、日志或二进制文件。

### 一手来源说了什么

1. [GitHub：About protected branches](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches) 说明可把状态检查设为合并前必需条件。分支保护是仓库设置，不会因为存在 workflow 文件就自动开启。
2. [GitHub Actions 事件文档](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows) 定义 `pull_request`、`schedule` 和手动 `workflow_dispatch` 等触发方式。
3. [GitHub 官方 `upload-artifact`](https://github.com/actions/upload-artifact) 说明 v4 以后上传的 artifact 是不可变的，并提供 artifact digest；同名制品不能在同一运行中被任意追加覆盖。

### 如何映射到本地实现

- [`.github/workflows/evaluation-regression.yml`](../../.github/workflows/evaluation-regression.yml) 在 PR、定时任务和手动触发时运行确定性测试、生成报告并执行回归比较；配置好模型与服务后还可以运行真实评测。
- [`internal/evaluation/gate/gate.go`](../../internal/evaluation/gate/gate.go) 要求基线与当前结果的 `ConfigHash` 非空且相同；阈值必须存在、指标名不能重复、要求的指标不能缺失，并同时汇总所有失败项。
- 真实评测基线按配置哈希保存在 `testdata/evaluation/live-baselines/`，工作流把结果作为 artifact 上传，便于复核当时到底比较了什么。

### 本项目自己的工程推导

- “配置哈希相等才允许比较”是本项目防止苹果和橘子混比的规则，并不是 GitHub Actions 自带能力。
- 把基线文件提交到 Git 并按配置哈希命名，是本项目选择的可审查基线方案；GitHub 的 artifact 不可变性只适用于某次工作流上传后的制品，不等于仓库内基线天然不可改。
- 质量指标通常是越高越好，时延/费用通常是越低越好，因此门禁支持两个比较方向和每指标独立阈值。

### 局限

- 管理员仍需在仓库设置中把对应工作流检查设为 required；仅提交 YAML 不会自动建立分支保护。
- 外部模型评测会受到网络、限流、模型更新和随机性的影响。确定性单元测试适合 PR 必跑，真实模型评测更适合独立凭据、可控预算、重试策略和审慎阈值。
- GitHub artifact 有保留期；若需要更强的长期供应链证明，可进一步采用 GitHub 的 [artifact attestations](https://docs.github.com/en/actions/concepts/security/artifact-attestations)，但当前项目没有宣称已经实现。

## 4. OpenAI、Anthropic 与 DeepSeek 的 Prompt Cache

### 陌生名词

**Prompt Cache（提示词缓存）**缓存的是模型处理长前缀时的中间计算。它通常要求请求开头完全一致，所以“稳定内容放前面、动态问题放后面”更容易命中。它与应用把完整回答存入 Redis 完全不同：命中后模型仍会生成新回答。

### 厂商官方机制与字段差异

| 厂商 | 官方机制 | 官方 usage 字段（当前文档） | 本项目要点 |
|---|---|---|---|
| OpenAI | [Prompt Caching 官方指南](https://developers.openai.com/api/docs/guides/prompt-caching) 说明缓存依赖相同前缀，并在支持的模型/请求上由服务端处理；工具定义等前缀内容也会影响命中。 | 新式响应使用 `input_tokens_details.cached_tokens`，文档也描述缓存写入 token 的计量；部分 OpenAI-compatible API 仍返回 `prompt_tokens_details.cached_tokens`。 | 兼容层必须容忍不同协议形态，不能把“字段缺失”当作 0 命中证据。 |
| Anthropic | [Prompt caching 官方文档](https://platform.claude.com/docs/en/build-with-claude/prompt-caching) 通过自动策略或 `cache_control` 断点识别可复用前缀；工具、system、messages 的顺序和前缀变化会影响缓存。 | `cache_creation_input_tokens` 表示写入，`cache_read_input_tokens` 表示读取，`input_tokens` 是断点后未缓存输入；总输入需按文档把三类相加。 | Anthropic 的“写、读、普通输入”拆分最明确，不能套用 OpenAI 的嵌套字段名。 |
| DeepSeek | [Context Caching 官方指南](https://api-docs.deepseek.com/guides/kv_cache/) 说明服务端默认尝试复用相同前缀，采用磁盘缓存且是尽力而为。 | `prompt_cache_hit_tokens` 与 `prompt_cache_miss_tokens`。 | DeepSeek 报告命中/未命中，没有与 Anthropic 等价的显式写入字段。 |

### 如何映射到本地实现

- [`internal/models/chat/prompt_cache.go`](../../internal/models/chat/prompt_cache.go) 把稳定的 system 消息和确定性工具定义组成“稳定前缀”，只保存哈希/长度等元数据，不保存正文。
- 同一文件把 OpenAI-compatible、DeepSeek、Anthropic 的不同 usage 字段归一到统一的缓存读取、写入和命中统计；字段不可用时保留“未报告/不支持”，而不是捏造命中。
- [`internal/models/chat/observability_wrapper.go`](../../internal/models/chat/observability_wrapper.go) 对普通和流式调用统一记录一次结果，保存耗时、usage、成功/错误类别、价格快照和关联 ID。
- [`internal/types/model_call.go`](../../internal/types/model_call.go) 的成本计算把普通输入、缓存读取、缓存写入和输出按各自价格类别分开。

### 本项目自己的工程推导

- “稳定前缀指纹”是 WeKnora 为排查缓存命中率而做的观测标识，**不是**通用厂商缓存键，也不会命令厂商一定命中。
- 将动态用户问题放到稳定 system/tool 前缀之后，是根据厂商“相同前缀”规则做的结构设计；不同 SDK 最终序列化方式仍可能影响实际命中。
- 归一化 usage 是为了统一成本和报表，不意味着三家语义完全相同。

### 局限

- 厂商的支持模型、最短前缀、保留时间和价格会变化，应以运行时所用模型对应的最新文档为准。
- 兼容 OpenAI 协议的第三方服务未必完全遵守 OpenAI 字段语义。
- 缓存命中只减少一部分前缀处理成本/时延，不保证回答相同，也不能代替应用级答案缓存。

## 5. 模型调用观测与 OpenTelemetry GenAI 语义约定

### 一手来源说了什么

1. [OpenTelemetry GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/) 为生成式 AI 的 spans、metrics 和 events 定义通用命名，目的是让不同厂商调用能用相近维度分析。
2. [OpenTelemetry GenAI spans](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/) 包括操作名、provider、请求/响应模型、输入/输出 token、缓存读写 token、耗时和流式首块时间等属性；同时强调 prompt/output 内容敏感，捕获内容应显式选择并受安全策略控制。

这些语义约定仍可能处于 Development 状态，字段会演进，不应把当前草案当作永不变化的 ABI。

### 如何映射到本地实现

- [`internal/types/model_call.go`](../../internal/types/model_call.go) 和对应迁移保存调用用途、provider/model、耗时、输入/输出/缓存 token、估算费用、成功状态、错误类别和关联 ID。
- 默认只记录 prompt 前缀哈希、内容哈希和计数，不把原始问题、答案或 API 密钥写入模型调用审计表，符合“内容属于敏感可选信号”的方向。
- 流式调用在结束或出错时汇总一次记录，避免每个数据块都变成一条重复计费记录。

### 本项目自己的工程推导

- `model_calls` 是 WeKnora 的持久化审计模型，字段命名和生命周期只是与 OpenTelemetry 概念对齐；当前实现不是 OpenTelemetry exporter，也不宣称通过语义约定一致性认证。
- 用 `purpose` 区分问答、评测、Embedding 等业务场景，用 correlation ID 串起评测运行和模型调用，是为课题三成本归因增加的领域维度。

### 局限

- 哈希能帮助判断“是否相同”，不能恢复正文，也不能解释语义质量。
- 持久化记录本身不等于完整的分布式追踪；跨服务传播 trace context、采样、指标聚合和告警仍需专门的遥测后端。
- 即使不保存正文，模型名、租户标识、错误类型和调用时间仍可能是敏感元数据，需要最小权限和保留期策略。

## 6. Embedding 缓存：正确性、数据库/Redis、租户隔离和并发合并

### 陌生名词

- **Embedding（向量嵌入）**：把文本转成一组浮点数，使语义相近的文本在向量空间中更接近。
- **Cache-aside（旁路缓存）**：应用先查缓存；未命中时访问原始服务，再把结果写回缓存。
- **缓存击穿/惊群**：许多并发请求同时未命中同一个键，于是一起调用昂贵的上游服务。
- **请求合并（singleflight）**：相同键同一时刻只执行一个上游请求，其他调用等待并共享结果。

### 一手来源说了什么

1. [OpenAI Embeddings 官方指南](https://developers.openai.com/api/docs/guides/embeddings) 说明请求由输入、embedding 模型以及可选输出维度等因素决定，向量长度会受到 `dimensions` 配置影响，输入按 token 计量。
2. [Redis 官方 Cache-Aside 指南](https://redis.io/docs/latest/develop/use-cases/cache-aside/) 描述先读缓存、miss 后读取数据源并回填的流程，也讨论 TTL、失效和并发 miss 的风险。
3. [Go `singleflight` 官方包文档](https://pkg.go.dev/golang.org/x/sync/singleflight) 定义了相同 key 的重复函数调用抑制：一个调用执行，重复调用等待并共享结果。
4. PostgreSQL 官方文档分别说明 [UNIQUE 约束](https://www.postgresql.org/docs/current/ddl-constraints.html)、[`INSERT ... ON CONFLICT`](https://www.postgresql.org/docs/current/sql-insert.html) 和 [Row Security Policies](https://www.postgresql.org/docs/current/ddl-rowsecurity.html)：前两者可保证数据库中的复合唯一性和冲突处理，RLS 可在数据库层按当前用户限制可见行。

### 如何映射到本地实现

- [`internal/types/model_call.go`](../../internal/types/model_call.go) 的缓存键由 `tenant + model + dimensions + model fingerprint + content hash` 构成。只用文本哈希会错误复用其他租户、模型或维度的向量。
- [`internal/models/embedding/cache_wrapper.go`](../../internal/models/embedding/cache_wrapper.go) 实现数据库 cache-aside：支持批量部分命中、保持原输入顺序、合并批内重复文本；miss 加进程内 keyed lock，拿锁后再次查询，减少同进程重复上游调用。
- [`internal/application/repository/embedding_cache.go`](../../internal/application/repository/embedding_cache.go) 查询时带齐租户和模型维度，并校验向量长度；写入使用复合唯一约束与 `ON CONFLICT DO NOTHING`，竞争写不会制造重复行。
- 数据库读取或写入缓存失败时回退到上游 Embedding，不让“优化层”破坏主要业务可用性。

### 本项目自己的工程推导

- 缓存键的完整组成是由“哪些输入会改变向量”和多租户安全共同推导的，不是某一家厂商规定的通用格式。
- `model fingerprint` 排除 API 密钥和价格，但纳入会改变结果的模型配置。这既避免密钥泄露，也避免价格变更让相同向量全部失效。
- 当前选数据库而不是 Redis，是为了复用现有持久层、减少部署依赖；Redis 官方文档只是未来高吞吐缓存层的参考，不代表本项目已接入 Redis。

### 局限

- keyed lock 只存在于当前 WeKnora 进程。多副本部署时，数据库唯一约束能防止重复落库，却不能阻止多个副本同时调用 Embedding 服务；若成本显著，可增加 Redis 分布式锁/租约或跨实例 singleflight，并处理超时和锁所有权。
- 当前租户隔离由应用查询条件和复合键执行，尚未使用 PostgreSQL RLS。应用层一旦漏带 tenant 条件仍有风险；RLS 是可选的纵深防御，不应未经完整迁移与连接身份设计直接开启。
- 内容哈希不应被视为加密存储；低熵短文本可能被猜测。权限、备份与保留期仍要保护缓存表。
- 本地 Embedding token 数使用字符数近似估算，因此费用不是账单级精度。

## 7. 模型定价快照与 Token 成本计算

### 一手来源说了什么

1. [OpenAI API Pricing](https://openai.com/api/pricing/) 以每百万 input/output token 等单位列价；[Prompt Caching 指南](https://developers.openai.com/api/docs/guides/prompt-caching) 又区分普通输入、缓存输入/写入和输出的计量类别。
2. [Anthropic Pricing 官方文档](https://platform.claude.com/docs/en/about-claude/pricing) 使用 MTok（百万 token）列出输入、输出以及 Prompt Cache 写入/读取等不同费率。
3. [DeepSeek Pricing 官方文档](https://api-docs.deepseek.com/quick_start/pricing/) 区分每百万输入 token 的 cache hit、cache miss 和输出 token，并提醒价格可能调整。

因此通用计算只能写成“用量 × 对应类别的单价 / 官方计价单位”，不能把三家所有 token 合并后乘一个单价。

### 如何映射到本地实现

- [`internal/types/model_call.go`](../../internal/types/model_call.go) 分别计算普通 miss 输入、cache read、cache write、输出和 Embedding 的每百万单位费用；rerank 则按项目价格模型中的每千次/每千单位口径计算。
- 每条调用记录保存当次使用的价格快照和估算费用。即使管理员以后修改模型价格，历史记录也不会按新价格被“重写”。
- 供应商未返回精确 usage 时使用估算值，并通过字段/状态区分，避免把估算伪装成厂商账单。

### 本项目自己的工程推导

- “调用时冻结价格快照”是审计需要推导出的设计：官方价格页给出当前计价单位，但不会替 WeKnora 保存历史合同价。
- 所有单价由部署者配置，本项目没有在运行时抓取官网价格，也没有自动换汇、税费、批处理折扣、阶梯价或企业合同价处理。
- 报表按业务 purpose、模型、评测运行等维度汇总，是为了回答“钱花在了哪里”，不属于厂商账单规范。

### 局限

- 官方价格会变化，企业合同也可能与公开价不同；上线前应把来源 URL、币种、单位、生效时间和录入人作为运维流程记录。
- Tokenizer、缓存语义和服务端计费调整会产生差异，最终结算必须以厂商账单为准。
- 当前 VLM、ASR、rerank 等不同模态/服务的计价单位未必都能被通用 token 模型精确表达，必须按供应商实际价格表扩展。

## 8. 数据库迁移与持久化

### 陌生名词

- **迁移（migration）**：按版本顺序改变数据库结构的脚本。
- **up/down**：up 向前安装一个版本，down 用于回退该版本。
- **事务（transaction）**：一组操作要么全部成功，要么全部失败，避免数据库停在半完成状态。

### 一手来源说了什么

1. [`golang-migrate/migrate` 官方项目](https://github.com/golang-migrate/migrate) 支持带版本的 up/down 迁移并覆盖 PostgreSQL、SQLite 等驱动；其 [Getting Started](https://github.com/golang-migrate/migrate/blob/master/GETTING_STARTED.md) 强调按顺序执行、测试迁移以及处理失败/dirty 状态。
2. [PostgreSQL 事务教程](https://www.postgresql.org/docs/current/tutorial-transactions.html) 说明事务把多个步骤组成原子操作，并在提交后对其他事务可见。
3. [SQLite 事务文档](https://www.sqlite.org/lang_transaction.html) 说明 SQLite 的自动事务、显式 BEGIN/COMMIT/ROLLBACK 以及读写事务行为。

### 如何映射到本地实现

- [`internal/database/migration.go`](../../internal/database/migration.go) 使用 `golang-migrate` 驱动执行版本化迁移，而不是应用启动时临时拼接建表 SQL。
- PostgreSQL 的课题三结构在 [`migrations/versioned/000090_evaluation_observability.up.sql`](../../migrations/versioned/000090_evaluation_observability.up.sql)，SQLite 对应结构在 [`migrations/sqlite/000013_evaluation_observability.up.sql`](../../migrations/sqlite/000013_evaluation_observability.up.sql)，并各有 down 文件。
- 迁移建立 `evaluation_runs`、`evaluation_cases`、`model_calls`、`embedding_cache` 及所需外键、复合唯一约束和查询索引；PostgreSQL 使用 JSONB，SQLite 使用 TEXT 保存 JSON，以适配两种数据库能力。
- 仓库测试覆盖新建数据库与升级路径，并验证模型记录、评测记录及缓存读写。

### 本项目自己的工程推导

- PostgreSQL 与 SQLite 分开维护等价迁移，是 WeKnora 同时支持生产/轻量环境的兼容策略，不是 `golang-migrate` 自动生成的。
- 评测配置和结果采用结构化列与 JSON 混合：常用筛选键单独建列/索引，易变化的快照放 JSON。这是查询性能与演进成本之间的权衡。
- 缓存写入的复合唯一约束既保证正确性，也让并发 `ON CONFLICT DO NOTHING` 成为安全的幂等写法。

### 局限

- down 迁移会删除课题三表和数据，只应在明确备份与恢复方案后执行。
- SQLite 与 PostgreSQL 的 JSON、并发写和约束行为并不完全相同；两套迁移语义必须分别测试，不能只验证“SQL 能执行”。
- `IF NOT EXISTS` 便于重复部署，但也可能掩盖一个结构不完整却同名的旧对象；升级测试必须进一步验证列、约束和索引，而不是只看迁移返回成功。

## 9. 评测任务生命周期、重启恢复与并发上限

### 为什么这是难点

一次真实 RAG 评测不是一个瞬间完成的 HTTP 请求。它要创建临时知识库、等待切块和向量化、并发运行多道题、调用多个模型、聚合结果，最后清理临时资源。最简单的实现是“把任务放在 Go 的 map 里，启动 goroutine 后立刻返回 task ID”，但它有三个问题：服务一重启历史状态就丢失；任务可能永远显示 `Running`；题目多时无限并发会抢占在线问答所需的模型和数据库资源。

### 一手来源与本地实现

- Go 官方 [`errgroup.Group.SetLimit`](https://pkg.go.dev/golang.org/x/sync/errgroup#Group.SetLimit) 支持限制同时活跃的 goroutine 数量。本项目在 [`internal/application/service/evaluation.go`](../../internal/application/service/evaluation.go) 中把离线评测限制为最多 4 个 worker，并继续让模型层自己的 governor 控制具体模型并发。
- [`internal/application/repository/evaluation.go`](../../internal/application/repository/evaluation.go) 将 `Pending → Running → Completed/Failed` 状态及时间、错误和结果写入数据库。服务启动时，`RecoverInterrupted` 会把上次进程遗留的 `Pending/Running` 标为失败并写明重启原因，而不是继续展示一个永远不会结束的假任务。
- [`internal/application/service/evaluation.go`](../../internal/application/service/evaluation.go) 使用临时知识库执行真实流水线，并在结束路径清理；逐题更新经过互斥保护并使用 upsert，使并发 worker 不会相互覆盖。

### 设计取舍与效果

- 数据库是任务历史的**事实来源**；内存对象只负责当前进程的快速并发协调。这样页面刷新和进程重启后仍能解释发生过什么。
- 重启恢复采用“标失败后允许重新发起”，没有尝试从任意中间步骤自动续跑。续跑需要每个阶段都有严格幂等性，复杂度和误复用风险更高，当前课题选择了更容易证明正确的失败恢复。
- 4 个 worker 是保守的进程级上限，不是永远正确的容量参数。生产环境仍应根据模型限流、数据库连接池和机器负载调整。

## 10. 逐题证据、稳定 passage ID 与重复文本

### 为什么只有总分不够

如果 Recall 从 0.80 变成 0.72，只保存总分无法回答“哪一道题、漏了哪一段、重排前还是重排后丢失”。因此每道题必须留下可复算证据：问题 ID、检索/重排 passage ID、最终答案、逐题指标、阶段耗时和错误。

另一个隐蔽问题是：向量库返回的是运行时 chunk，而数据集标注使用的是 passage ID。若直接把 `ChunkIndex` 当 passage ID，或者只在“标准答案段落”中做文本匹配，就可能让不相关结果凭空消失，Precision 被虚高；内容完全相同的两段还可能映射错身份。

### 本地实现

- [`internal/application/service/knowledge_process.go`](../../internal/application/service/knowledge_process.go) 在把数据集 passage 变成 chunk 时写入原始序号元数据。
- [`internal/application/service/metric_hook.go`](../../internal/application/service/metric_hook.go) 优先读取该稳定身份；旧数据缺少元数据时才回退到对**完整语料库**做内容匹配。完整语料库很重要：错误召回也必须被保留，才能正确计算 Precision。
- [`internal/application/service/metric_hook_test.go`](../../internal/application/service/metric_hook_test.go) 覆盖重复文本和错误召回，防止以后又退回到“只匹配 qrels”的错误算法。
- 逐题结果保存到 `evaluation_cases`，并以 `(task_id, qid)` 唯一；[`internal/application/repository/evaluation.go`](../../internal/application/repository/evaluation.go) 负责 upsert 和租户范围查询。

### 边界

- passage ID 是“本数据集版本中的身份”，不是跨数据集的永久全局 ID；换数据集版本后必须依赖数据集哈希一起解释。
- 回退的内容包含匹配是兼容旧数据的保底方式，不如显式元数据可靠；如果 passage 是另一个 passage 的子串，可能出现多义映射。因此新评测数据应始终携带原始 passage 序号。

## 11. 状态接口与逐题分页：避免轮询产生平方级传输

### 为什么这是难点

`evalctl` 会反复轮询任务状态。若状态接口每次都返回“当前已有的全部逐题结果”，1000 道题执行过程中可能先传 1 条、再传 2 条……最后传 1000 条，累计数据量近似 `1+2+...+1000`，也就是平方级增长。这既浪费网络，也让数据库和 JSON 序列化越来越重。

### 本地实现

- [`internal/handler/evaluation.go`](../../internal/handler/evaluation.go) 的状态响应只返回运行摘要；逐题结果由独立 `/evaluation/cases` 接口按 `limit/offset` 获取，并限制参数范围。
- [`cmd/evalctl/main.go`](../../cmd/evalctl/main.go) 等任务结束后才以每页 100 条拉取所有 cases，再生成详细 JSON 和 Markdown 报告。
- [`cmd/evalctl/main_test.go`](../../cmd/evalctl/main_test.go) 使用 205 条数据验证确实会翻三页，避免“只取第一页但报告看似成功”的静默丢数。

### 取舍

当前采用 offset 分页，逻辑简单，适合评测结束后按稳定顺序读取。若单次任务未来达到数十万条，游标分页会比大 offset 更合适；这属于规模增长后的优化，不应在没有压力证据时先增加协议复杂度。

## 12. 统一模型观测包装器、流式只记一次与取消安全

### 陌生名词

- **装饰器/包装器（decorator/wrapper）**：不改模型提供商内部实现，而是在其外层统一增加计时、计数、错误记录等能力。
- **流式响应（streaming）**：模型不是一次返回完整答案，而是不断发送小片段。
- **exactly once（逻辑上只记录一次）**：一个模型调用无论产生多少流片段，最终只形成一条计费/观测记录。

### 为什么这是难点

WeKnora 有 Chat、Embedding、Rerank、VLM、ASR 五类接口，且厂商适配器很多。若在每个厂商实现里各写一套费用逻辑，很快会出现字段、错误处理和成本公式不一致。Chat 流式调用更容易出错：每个 chunk 都记一次会重复计费；只看流正常关闭又会漏掉中途的错误事件；用户取消请求后直接沿用原 context，失败记录可能也随之被取消。

### 一手来源与本地实现

- Go 官方 [`context.WithoutCancel`](https://pkg.go.dev/context#WithoutCancel) 会保留父 context 的值，但不继承取消信号。本项目的 [`internal/types/model_call.go`](../../internal/types/model_call.go) 在它外面再加 5 秒超时：保留租户、purpose 和关联 ID，同时确保落库不会无限挂起。
- 五类模型包装器位于 `internal/models/*/observability_wrapper.go`，由 [`internal/application/service/model.go`](../../internal/application/service/model.go) 在模型构造边界统一装配；领域模型和厂商 SDK 不直接依赖 GORM。
- [`internal/models/chat/observability_wrapper.go`](../../internal/models/chat/observability_wrapper.go) 转发每个流片段，只保留最后观测到的 usage；流关闭后统一记录一条。若出现错误事件，记录为失败，但不保存可能包含敏感正文的供应商错误 body。
- [`internal/types/context_helpers.go`](../../internal/types/context_helpers.go) 将评测 task ID 放入 context，使本次 RAG 内部的 Chat/Embedding/Rerank 调用都能通过 `correlation_id` 回到同一评测任务。

### 局限

- 这是应用级 exactly-once 目标，不是分布式事务保证。若进程在厂商调用成功后、数据库提交前崩溃，仍可能漏记；若业务层重试整个请求，也会形成两次真实调用和两条记录。
- VLM 接口目前拿不到厂商 token，按字符/图片数量估算并明确标记；ASR 记录字节和耗时，但尚未表达所有厂商的按分钟计费。不能把这些数字冒充精确账单。

## 13. 模型配置更新兼容：字段“没传”和“主动清空”不是一回事

### 为什么这是难点

价格字段是在旧版模型 API 之后新增的。旧前端更新模型时可能根本不发送 `pricing`；如果后端把整个 `parameters` 当完整替换，就会无意清空管理员已经录入的价格。反过来，如果后端总是继承旧价格，新前端又无法显式清空错误价格。

### 本地实现

- [`internal/handler/model.go`](../../internal/handler/model.go) 为更新请求自定义 `UnmarshalJSON`，额外记录 JSON 中是否真的出现过 `parameters.pricing`。
- 字段省略：继承数据库中旧价格，兼容旧客户端。
- 明确发送空对象：按用户意图清空为零价格。
- 明确发送非空对象：规范化币种并校验单价不能为负数。
- 修改或删除模型时，[`internal/application/service/model.go`](../../internal/application/service/model.go) 还会使该模型派生的 Embedding 缓存失效，避免旧配置向量长期占用或被误用。

这个“presence-aware update”是本项目为 API 演进做的兼容设计，不是某个模型厂商的要求。它体现了一个通用后端原则：在 PATCH/兼容更新语义中，`missing`、`null`、`{}` 和具体值可能代表不同意图。

## 14. 多租户权限、前端集成与端到端验证

### 多租户和权限为什么必须贯穿全链路

只在前端隐藏按钮不是安全控制。费用、评测答案、模型 ID 和缓存统计都属于租户数据；后端路由、context、repository 查询和数据库唯一键必须一致携带 tenant ID。

- [`internal/router/routes_infra.go`](../../internal/router/routes_infra.go) 把运行评测限制为 Admin，把结果和费用查看限制为租户 Viewer，并为 API Key 使用 `run_evaluations`/完整访问能力范围。
- `EvaluationRepository`、`ModelCallRepository` 和 `EmbeddingCacheRepository` 的读写都带租户条件；缓存键把 tenant 放在第一维，不能跨租户复用向量。
- `model_calls` 不保存提示词、答案正文或密钥；`evaluation_cases` 为复算质量保存固定测试集的最终答案和 passage ID，因此它比普通遥测更敏感，必须继续受租户权限保护。

### 前端并不只是“加四个数字”

- [`frontend/src/views/settings/ModelSettings.vue`](../../frontend/src/views/settings/ModelSettings.vue) 提供模型/日期筛选，展示调用数、Prompt Cache 读取 token、Embedding 命中率和估算费用。
- [`frontend/src/components/ModelEditorDialog.vue`](../../frontend/src/components/ModelEditorDialog.vue) 为支持计价的模型提供价格输入；[`frontend/src/utils/modelPricing.ts`](../../frontend/src/utils/modelPricing.ts) 负责表单归一化与序列化，避免把空字符串错误变成无效数字。
- 四种语言的 i18n 键同步增加；VLLM 也属于 Chat 类型计价，不能因界面类型判断遗漏。

### 验证策略

本次不是只验证“能编译”，而是按风险分层：

1. 纯函数测试：配置哈希、成本公式、指标注册表和门禁方向。
2. Repository 测试：租户隔离、upsert、分页、聚合、失效和重启恢复。
3. 并发/流测试：批量部分命中、顺序保持、同键合并、流错误与取消落库。
4. 迁移测试：PostgreSQL/SQLite 新建与升级结构。
5. 集成检查：目标 Go 测试、`go test -race`、`go vet`、前端测试、类型检查、i18n 检查和生产构建。
6. CI 门禁实验：正常报告通过；故意降低 Recall 的报告必须失败。

Go 官方[数据竞争检测器](https://go.dev/doc/articles/race_detector)只能发现执行路径实际触发的数据竞争，不能证明不存在所有并发错误。因此竞态测试与数据库唯一约束、keyed lock、幂等写入测试必须组合使用。完整已执行结果见 [`TEST_REPORT.md`](TEST_REPORT.md)，真实模型环境限制见 [`EXPERIMENT_REPORT.md`](EXPERIMENT_REPORT.md) 和 [`LIMITATIONS.md`](LIMITATIONS.md)。

## 15. 这些参考资料究竟影响了什么，没有影响什么

| 本项目决策 | 外部参考提供的部分 | 本项目自行设计的部分 |
|---|---|---|
| 数据集与配置可复现 | SHA-256 的标准定义；实验跟踪记录参数/指标/代码版本的思想 | manifest 格式、二次校验、ConfigHash 字段和排除 CodeVersion 的规则 |
| 评测指标 | TREC 定义、NDCG/BLEU/ROUGE 原始论文 | 中文分词、平滑、二元相关性、指标注册表和当前项目口径 |
| CI 门禁 | GitHub Actions 触发器、artifact、required status check | 同配置才能比、阈值方向、fail-closed 和哈希命名基线 |
| Prompt Cache | OpenAI/Anthropic/DeepSeek 的稳定前缀和 usage 字段 | 统一归一化结构、稳定前缀指纹、费用归因和隐私取舍 |
| Embedding 缓存 | cache-aside、singleflight、数据库 unique/upsert | 五维缓存键、批量重排、配置指纹、失效时机和降级策略 |
| 模型观测 | OpenTelemetry GenAI 的跨厂商维度和敏感内容警告 | `model_calls` 表、五类包装器、task correlation 和 5 秒取消安全落库 |
| 费用 | 厂商公开计价类别和单位 | 用户维护价格、调用时快照、统一估算公式和页面聚合 |
| 持久化 | 迁移、事务、唯一约束的一般机制 | 运行/case/call/cache 四表边界、双数据库迁移和重启恢复策略 |

所以准确的说法不是“照着某个著名厂商抄了一套”，而是：先从官方资料确认各个机制的事实边界，再按照 WeKnora 已有的 Go 分层、模型接口、租户 context、repository、迁移和 Vue 页面结构，把这些机制组合成适合课题三的工程方案。

## 验收时如何使用这份报告

建议按下面四个问题逐项追踪：

1. **比较对象相同吗？** 查看 dataset hash、config hash、模型快照和 code version。
2. **指标口径相同吗？** 特别确认 `k`、相关性等级、分词、MAP 分母、BLEU/ROUGE 实现版本。
3. **成本来源可解释吗？** 查看厂商 usage 原字段、归一化字段、是否估算、价格快照单位与时间。
4. **证据能留下吗？** 查看数据库运行记录、CI 报告/artifact、基线文件及必需状态检查。

只要其中一个问题答不清，就不应把两个评测分数或费用数字直接放在一起下结论。
