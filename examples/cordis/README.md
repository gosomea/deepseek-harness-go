# Cordis 示例

## 职责

以计数器 Provider 和 greeter Consumer 展示等待依赖、激活、服务消失时卸载、服务恢复时重新激活，以及监听器随所属插件清理。

## 使用

在项目根目录运行 `go run ./examples/cordis`。完整代码见 [main.go](main.go)，输出见 [expected.txt](expected.txt)，操作讲解见 [教程](../../docs/cordis/tutorial.md)。

## 限制

这是内存中的生命周期示例，不连接模型或外部服务。计数器仅供这个串行 Emit 示例使用；改成 Parallel 时需要为其状态增加同步。

## 验证

运行 `go test ./examples/cordis`。测试执行同一个 `run` 函数并逐字比较输出快照；`make check` 包含此测试。更新输出时同步审阅教程中的行为解释。
