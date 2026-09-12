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

本地自动化没有使用或输出用户的 DeepSeek 密钥，也没有把当前正在运行的旧镜像结果冒充成此分支结果。因此这里不填写虚构的 BLEU、ROUGE、费用或厂商缓存 token。

真实前后对比应在构建并启动本分支服务后按 [运行手册](RUNBOOK.md) 执行两次：第一次记录冷缓存，第二次记录热缓存。将两份 `evaluation-detail.json` 放入评审制品，比较以下字段：

- 质量：Recall、MRR、NDCG、ROUGE-L。
- 调用：Embedding `cache_hit_items` / `cache_miss_items`。
- 成本：总成本及 Chat/Embedding/Rerank 分项。
- 耗时：索引、检索、重排、生成、总耗时、平均逐题耗时和模型调用耗时。

接受标准是第二次 Embedding 调用减少，同时质量指标不超过门禁允许退化范围。

## 迁移后的补充证据

围绕 Embedding、普通 RAG 稳定前缀、Wiki 缓存布局和供应商 Prompt Cache usage 解析，已在竞态检测下执行 12 项聚焦测试并全部通过。完整命令输出保存于 `artifacts/migration-cache-deterministic-tests.txt`，摘要保存于 `artifacts/migration-cache-deterministic-evidence.json`。这组证据是 hermetic 的代码契约证明，不等同于真实供应商账单实验。

当前分支服务未能完成本机冷/热运行。Docker Desktop 在 Linux engine 启动前因本地 Secrets Engine 套接字异常退出，因此没有创建评测任务、没有发送供应商调用，也没有产生费用。本轮状态明确记为 `inconclusive`，见 `artifacts/migration-cache-live-status.json`；历史 `code_version=unknown` 报告没有被冒充为当前提交证据。

可选解析器基准使用独立审核的七文档语料运行了固定 56 单元矩阵。本机只有 `simple` 引擎可用，最终为 `partial`、`1/8` ready、2 个单元计分、0 个失败、35 个 unavailable、19 个 unsupported。两份已计分 Markdown 单元在适用分量上均为 1；该结果只证明实际可用集合，不能声称完成了八个引擎的实测比较。
