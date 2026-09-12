# 选择性迁移代码审查记录

审查日期：2026-09-12

本轮对迁移后的工作树进行了双轴复核：一条检查仓库工程规范，一条检查迁移计划、课题需求与 `SPEC.md` 的符合性。两条审查均以目标分支源码、测试与产物为证据，最终结论均为无剩余 finding。

## 审查中发现并修复的问题

1. Parser authoring ID 可以构造越界路径。现在只接受冻结语料清单中的精确 ID/格式，并验证所有输出路径位于指定根目录内。
2. 整体 deadline 在首个单元前触发时，ready 引擎可能被错误汇总为 unavailable。现在发现阶段的 availability 会进入结果，取消前未执行的单元使用明确的 typed outcome。
3. DOCX ZIP 与 PNG 编码错误曾被吞掉，标准库压缩输出还会随 Go/OS 变化。现在生成链完整传播错误，使用确定性 RGB PNG、fixed-Huffman DEFLATE 与 ZIP Store 编码，并固定合法 DOS 时间和逐成员 CRC/完整读取验证。
4. 缺失价格会形成 estimated zero，旧门禁可能把它当作有效成本。现在报告记录价格覆盖率和门禁资格；成本阈值缺少资格元数据、覆盖不完整、覆盖率越界或元数据矛盾时全部 fail closed。

## 验证

- 迁移相关 Go 包、CLI、缓存和稳定前缀测试在 `go test -race` 下通过。
- Wiki 缓存布局、并发 warmup 与取消清理的定向竞态测试通过。
- 6 项本地策略门禁通过，相关包 `go vet` 通过。
- `git diff --check` 通过；修改/新增 JSON、YAML 与 Markdown 相对链接校验通过。
- 审查后的原始聚焦测试输出保存在 `artifacts/migration-final-focused-tests.txt`。
- Linux amd64、Go 1.26 环境的 `go test -p 2 ./...` 最终全仓通过；两个失败轮次与修复后的通过轮次原始日志均保存在 `artifacts/migration-linux-go-test-all*.txt`。全仓 `go vet -p 2 ./...` 的独立 Linux Go 1.26.8 日志为 `artifacts/migration-linux-go-vet-all.txt`，退出码 0。
- 生产镜像 `sha256:aa53e1d16c3554b5f06f2dff9ce07fff0cc158601f496daa2267da4a32f22267` 已替换运行容器，二进制嵌入提交 `c88047bf0285a1ded72f06ff038f3c1679b2ef2d`，后端健康检查和前端 HTTP 检查均通过。

## 结论

选择性迁移实现符合既定架构边界：继续复用 0828 的评测、调用观测、缓存和 CI 主链，未引入第二套六表调用记录模型或 runtime factory。真实 RAG、Wiki 和 parser 的限制仍按 `partial`、`not_comparable` 或 `unknown` 如实保留。
