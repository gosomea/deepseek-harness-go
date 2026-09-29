# 开发说明

## 环境

Go 1.22 或更新版本。运行时与仓库门禁仅用标准库；macOS/Linux 可用 Make 聚合检查。CI 配置包含 Linux、macOS 和 Windows。当前的验收证据只来自本机，远端 CI 状态另行记录。

仓库通过 `.gitattributes` 将文本检出为 LF，包括 Go 源码、生成文档与输出快照，避免 Windows 的 CRLF 转换改变格式检查或逐字比较结果。CI 各平台统一使用 Bash 执行 Go 命令，避免 PowerShell 拆分带点号的 flag 值。各平台独立完成检查；任一平台失败不会取消其他平台。

## 命令

| 命令 | 用途 |
| --- | --- |
| `make check` | 执行当前阶段全部必要门禁 |
| `go test -race -count=1 ./cordis` | 聚焦运行时的生命周期与竞态测试 |
| `go test ./scripts/doccheck` | 门禁有效/无效样例 |
| `make api` | 根据 Go 注释重新生成 API，然后检查文档 |
| `make docs` | 检查包说明、公开注释、链接和 API 新鲜度 |
| `make demo` | 运行 Cordis 完整示例 |
| `go doc ./cordis` | 查看本地 Go 包接口 |

`make coverage` 读取已有 `coverage.out`；先执行 `make test` 产生报告。Makefile 禁用并行执行，确保 `make check` 先产生覆盖率报告再检查它。具体门禁要求见 [测试文档](testing.md)。

## 新增模块

建立独立领域目录，写包注释和包含职责、使用、限制、验证四节的 README。为 Provider 与 Consumer 定义小接口及完整示例。把使用文档加入 [索引](README.md)，把阶段状态放入 [路线图](roadmap.md) 对应计划。新增公开核心包时扩展 API 生成器；新增运行时包时扩展覆盖率门禁的包选择。

## 调试生命周期

先从 `root.Fibers()` 查看状态与错误。Pending 表示父实例或注入服务尚未可用，Failed 表示 Apply 失败后的回滚已完成；注册前的配置错误直接返回，不创建实例。外部装配结束后调用 `root.Wait(ctx)` 观察结算后的状态。需要人工插入日志时通过 `ctx.Logger()` 标注插件名。

应用注册的资源要用 `OnDispose` 或 Apply 返回值加入所属激活。对自己创建的 goroutine，监听 `ctx.GoContext().Done()`，并在 Cleanup 中等待其退出。详细时序见 [生命周期](cordis/lifecycle.md)。
