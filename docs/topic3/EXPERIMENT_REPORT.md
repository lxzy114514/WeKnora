# 实验报告

## 实验目标

验证三件事：固定条件可比较、Embedding 缓存确实减少模型调用、提示词稳定前缀不会随动态上下文漂移。所有数字必须能由仓库中的测试或真实评测报告复算。

## 确定性实验结果

### 1. 数据集完整性

`baseline-v1` 清单覆盖 5 个 Parquet 文件。测试会重新计算大小和 SHA-256；修改任意文件后，快照加载失败，而不是继续生成不可比较的结果。题目按 QID 排序，避免文件读取顺序改变执行顺序。

### 2. Embedding 重复批次

测试输入先为 `[alpha, beta]`，再为 `[beta, alpha]`。

| 指标 | 第一次 | 第二次 | 结论 |
| --- | ---: | ---: | --- |
| 新增 provider 批量调用 | 1 | 0 | 相同模型、文本、维度被复用 |
| 缓存命中条目 | 0 | 2 | 第二次全部命中 |
| 输出顺序 | alpha, beta | beta, alpha | 与各自输入顺序一致 |

### 3. 并发相同输入

8 个并发单条请求只触发 1 次 provider `Embed`。两个重叠批次在第一个 provider 请求未结束时同时到达，也只触发 1 次批量 provider 调用。该结果证明并发协调发生在调用前，而不只是依赖数据库唯一约束消除重复行。

### 4. 隔离与失效

Repository 与包装器测试覆盖租户、模型 ID、维度和文本哈希组成的完整键。相同文本在任一维度变化时不会错误复用。数据库只保存 SHA-256 与向量，断言中不存在原文。

### 5. 提示词稳定前缀

两个普通 RAG 请求使用不同问题和不同检索上下文时：

- 实际消息内容保持原模板渲染结果，不改变模型语义。
- `PromptPrefixFingerprint` 相同。
- 指纹输入不包含问题或检索文本。

Wiki 路径原有测试同时验证用途和稳定前缀元数据会传到模型层。

### 6. 当前代码生成的回归门禁

`evalbench` 使用当前提交编译出的 Precision、Recall、MRR、NDCG、BLEU、ROUGE 实现，对两个固定证据用例生成 12 项指标；再与 `deterministic-baseline.json` 比较 Recall、MRR、NDCG@10 和 ROUGE-L，4 项检查全部通过。故意退化样例 Recall 为 `0.60`，政策门禁返回非零并明确命名 `retrieval.recall`。真实检索链路由配置了当前分支预览部署的 `Live RAG quality gate` 覆盖，不把两个静态报告互比冒充当前代码评测。

### 7. 检索误命中计分

增加一个相关 passage 与一个无关 passage 同时被召回的用例。逐题 Precision 必须为 `0.5`、Recall 为 `1.0`。该测试防止旧式“只在正确 passage 中反查 ID”把无关结果丢掉、从而把 Precision 虚高为 `1.0`。

## 真实模型实验状态

所有真实模型实验均使用当前分支构建的服务，自动化只调用已保存凭据且没有读取、输出或写入 DeepSeek 密钥。确定性测试、真实冷/暖运行和一次性货币层验证分别保存原始证据，不以旧镜像或虚构数字替代。

真实前后对比应在构建并启动本分支服务后按 [运行手册](RUNBOOK.md) 执行两次：第一次记录冷缓存，第二次记录热缓存。将两份 `evaluation-detail.json` 放入评审制品，比较以下字段：

- 质量：Recall、MRR、NDCG、ROUGE-L。
- 调用：Embedding `cache_hit_items` / `cache_miss_items`。
- 成本：总成本及 Chat/Embedding/Rerank 分项。
- 耗时：索引、检索、重排、生成、总耗时、平均逐题耗时和模型调用耗时。

接受标准是第二次 Embedding 调用减少，同时质量指标不超过门禁允许退化范围。

## 迁移后的补充证据

围绕 Embedding、普通 RAG 稳定前缀、Wiki 缓存布局和供应商 Prompt Cache usage 解析，已在竞态检测下执行 12 项聚焦测试并全部通过。完整命令输出保存于 `artifacts/migration-cache-deterministic-tests.txt`，摘要保存于 `artifacts/migration-cache-deterministic-evidence.json`。这组证据是 hermetic 的代码契约证明，不等同于真实供应商账单实验。

Docker 恢复后已用当前提交镜像完成一次冷运行和一次热运行，两者配置哈希相同，服务报告 `code_version=db7ca387210c`。Embedding 从冷运行 0/5 命中、5 个未命中变为热运行 5/5 命中、0 个未命中；Chat cache-read tokens 从 0 增至 1024，cache-miss tokens 从 1137 降至 113。检索指标保持不变，ROUGE-L 变化约 -0.006；这证明应用缓存与供应商提示词缓存均在实际链路生效，但暖跑 provider 新计算不为零，严格可比结论为 `not_comparable`。

完整门禁没有通过：热运行的远端生成更慢，使 average case latency 增加 13,917 ms。两类模型的价格快照为空，报告中的零费用均标记 estimated，不能解释为真实免费或金额节省；生成的原始 gate 虽把数值零判为 passed，复核后的有效成本结果是 `inconclusive`。因此本轮总状态为 `partial`，精确事实见 `artifacts/migration-live-cold-warm-comparison.json`；历史 `code_version=unknown` 报告没有被冒充为当前提交证据。

### DeepSeek 一次性货币层验证

2026-09-12 仅对 `deepseek-v4-pro` 配置了当前官方谷价时段费率。配置通过 `PUT /api/v1/models/:id` 写入，随后完成一次模型调试和一轮 `baseline-v1` 小规模评测；所有实际调用均落在同一 UTC 价格时段内。模型调试、文档摘要和知识问答共 3 条新增 DeepSeek 调用，均满足 `cost > 0` 与 `cost_estimated=true`，模型用量 Summary 聚合为 USD 0.003405424。

小规模评测报告自身的总费用为 USD 0.002908708，`priced_call_count=3/3`、`cost_coverage_ratio=1`、`cost_gate_eligible=true`。其中两条 Embedding logical call 全部命中应用缓存，没有发生 provider 输入，按成本合同属于已知零；这不等于本地 Ollama embedding 已有 token 价格。若未来出现 embedding cache miss，该部分费用仍为 `unknown`。

DeepSeek 当前价格随 UTC 峰谷时段变化，而 `ModelPricing` 只能保存一套静态费率。验证结束后，请求显式携带 `pricing: {}` 恢复空配置，API 回读为 `{}`；临时认证身份也已撤销且复用请求返回 401。运行前 41 条历史 `model_calls` 的计数与校验和在运行后保持不变。官方价格快照和数据库/API 审计链分别保存于 `artifacts/pricing-snapshot-20260912.md` 与 `artifacts/pricing-currency-evidence-20260912.json`。本实验没有重跑 A4，也不提供冷/暖金额差。

### Wiki 真实冷/暖链路

在独立临时 Wiki 知识库中先后导入内容相同、标题不同的两份手工 Markdown，并等待解析、摘要和 Wiki pipeline 全部结束。生产异步 Wiki 调用没有写入 correlation ID，因此证据使用两个 knowledge ID 与互不重叠的受控时间窗圈定，并在 JSON 中保存全部 `purpose LIKE 'wiki_%'` 行。

冷跑 9 次 Wiki 模型调用，cache-read/cache-miss tokens 为 2,560/11,027；暖跑 11 次，为 6,784/12,078，读取比例从 0.188415 升至 0.359665。真实 Wiki 路径的缓存使用得到证明。由于第二轮触发了 deduplication、多一次 page modification，调用形状不同，暖跑仍有新计算，且所有价格快照不完整，所以结论为 `partial / not_comparable`，费用为 `unknown`。临时知识库和测试身份在取证后已清理；逐调用证据保存于 `artifacts/migration-wiki-cold-warm-evidence.json`。

可选解析器基准使用独立审核的七文档语料运行了固定 56 单元矩阵。本机只有 `simple` 引擎可用，最终为 `partial`、`1/8` ready、2 个单元计分、0 个失败、35 个 unavailable、19 个 unsupported。两份已计分 Markdown 单元在适用分量上均为 1；该结果只证明实际可用集合，不能声称完成了八个引擎的实测比较。
