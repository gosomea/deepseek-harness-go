# 跨平台检出修复验证

首次远端 CI：[36598973071](https://github.com/gosomea/deepseek-harness-go/actions/runs/36598973071)。Windows 的格式检查失败，Linux 通过，macOS 被矩阵提前取消。

本机临时 clone 设置 `core.autocrlf=true`：原配置检出 `w/crlf`，15 个 Go 文件需要格式化；增加 LF 属性并重新检出后为 `w/lf`，0 个文件需要格式化。工作流同时增加独立完成矩阵与失败文件输出。

以下为原门禁的重新验收；语义审阅 `independent: false`。修复后的远端状态以对应提交的 GitHub Actions 为准。

## foundation

命令：`go version`；退出码：`0`。

```text
go version go1.22.12 darwin/amd64
```

命令：`go list ./...`；退出码：`0`。

```text
github.com/gosomea/deepseek-harness-go/cordis
github.com/gosomea/deepseek-harness-go/examples/cordis
github.com/gosomea/deepseek-harness-go/scripts/doccheck
```

## runtime

命令：`go test -race -count=1 -timeout=60s ./cordis ./examples/cordis`；退出码：`0`。

```text
ok  	github.com/gosomea/deepseek-harness-go/cordis	1.789s
ok  	github.com/gosomea/deepseek-harness-go/examples/cordis	1.959s
```

## acceptance

命令：`make check`；退出码：`0`。

```text
go vet ./...
go test -race -count=1 -timeout=60s -coverprofile=coverage.out ./...
ok  	github.com/gosomea/deepseek-harness-go/cordis	1.817s	coverage: 97.7% of statements
ok  	github.com/gosomea/deepseek-harness-go/examples/cordis	2.016s	coverage: 73.2% of statements
ok  	github.com/gosomea/deepseek-harness-go/scripts/doccheck	2.484s	coverage: 90.0%
go run ./scripts/doccheck -coverage coverage.out
Cordis coverage: 97.7%
documentation gate passed
go run ./scripts/doccheck
documentation gate passed
go build ./...
```
