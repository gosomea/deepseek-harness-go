# 本机门禁执行日志

环境：Go 1.22.12，darwin/amd64；宿主 macOS 15.3.1。本次验收未运行远端 CI。

语义审阅由 codex-root 在检查完成后执行，`independent: false`。核对内容包括模块依赖方向、资源所有者、消费者优先清理、配置失败回滚、并发等待条件、公开文档及未实现范围。

## foundation

### go-version

命令：`go version`；退出码：`0`。

```text
go version go1.22.12 darwin/amd64
```

### module

命令：`go list ./...`；退出码：`0`。

```text
deepseek-harness-go/cordis
deepseek-harness-go/examples/cordis
deepseek-harness-go/scripts/doccheck
```

## runtime

### runtime-tests

命令：`go test -race -count=1 -timeout=60s ./cordis ./examples/cordis`；退出码：`0`。

```text
ok  	deepseek-harness-go/cordis	1.850s
ok  	deepseek-harness-go/examples/cordis	2.031s
```

## acceptance

### make-check

命令：`make check`；退出码：`0`。

```text
go vet ./...
go test -race -count=1 -timeout=60s -coverprofile=coverage.out ./...
ok  	deepseek-harness-go/cordis	2.449s	coverage: 97.7% of statements
ok  	deepseek-harness-go/examples/cordis	1.774s	coverage: 73.2% of statements
ok  	deepseek-harness-go/scripts/doccheck	2.159s	coverage: 90.0%
go run ./scripts/doccheck -coverage coverage.out
Cordis coverage: 97.7%
documentation gate passed
go run ./scripts/doccheck
documentation gate passed
go build ./...
```

## 证据入口

[结构化报告](report.json) 包含输入/产物 SHA-256、检查结果与语义审阅；[报告说明](report.md) 是 CLI 生成的入口。完整输出也以原始 .stdout/.stderr 文件保留。
