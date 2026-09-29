# 文档门禁工具

## 职责

检查每个非测试 Go 包的 README、包注释、公开声明和字段注释；检查 Markdown 本地文件链接及标题片段；生成并校验 Cordis API；检查 Cordis 的语句覆盖率下限。

## 使用

```sh
go run ./scripts/doccheck
go run ./scripts/doccheck -write-api
go run ./scripts/doccheck -coverage coverage.out
```

从项目根目录运行，也可以用 `-root` 指定待检查目录。规则由 [测试文档](../../docs/testing.md) 维护。该工具不依赖 Python 或第三方解析器。

## 限制

本地链接检查覆盖行内 Markdown 链接与普通标题片段；不检查远端 URL 的可用性、引用式链接、重复标题的编号规则或网站生成。API 检查覆盖当前 `cordis` 包，新增公共核心包时必须扩展生成入口。注释存在性检查不能判断语义是否准确，评审仍要核对行为、时序和失败说明。

## 验证

运行 `go test ./scripts/doccheck`。有效/无效样例验证公开注释、包 README、目标文件、标题片段、目录逃逸、API 过时与覆盖率不足的拒绝路径。它们由 `make check` 执行；改变规则时同步修改这些测试。
