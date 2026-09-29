# GitHub 发布准备：本机执行日志

Go module 已迁移为 `github.com/gosomea/deepseek-harness-go`。本日志记录推送前的本机门禁；远端 CI 结果以 GitHub Actions 对应提交为准。语义审阅 `independent: false`。

## foundation

### go-version

命令：`go version`；退出码：`0`。

```text
go version go1.22.12 darwin/amd64
```

### module

命令：`go list ./...`；退出码：`0`。

```text
github.com/gosomea/deepseek-harness-go/cordis
github.com/gosomea/deepseek-harness-go/examples/cordis
github.com/gosomea/deepseek-harness-go/scripts/doccheck
```

## runtime

### runtime-tests

命令：`go test -race -count=1 -timeout=60s ./cordis ./examples/cordis`；退出码：`0`。

```text
ok  	github.com/gosomea/deepseek-harness-go/cordis	1.859s
ok  	github.com/gosomea/deepseek-harness-go/examples/cordis	1.983s
```

## acceptance

### make-check

命令：`make check`；退出码：`0`。

```text
go vet ./...
go test -race -count=1 -timeout=60s -coverprofile=coverage.out ./...
ok  	github.com/gosomea/deepseek-harness-go/cordis	1.954s	coverage: 97.7% of statements
ok  	github.com/gosomea/deepseek-harness-go/examples/cordis	2.158s	coverage: 73.2% of statements
ok  	github.com/gosomea/deepseek-harness-go/scripts/doccheck	1.668s	coverage: 90.0%
go run ./scripts/doccheck -coverage coverage.out
Cordis coverage: 97.7%
documentation gate passed
go run ./scripts/doccheck
documentation gate passed
go build ./...
```

[结构化报告](report.json) 保留输入/输出指纹与审阅记录。
