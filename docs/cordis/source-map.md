# Cordis 源码对应与实现范围

## 参考来源

参考本机 `/Users/yuqixian/forever-skills/projects/agent-research/deepseek-harness/vendor/cordis`，版本 `@deepseek-ai/cordis` 4.0.4，Git 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`。读取时 `vendor/cordis` 没有未提交修改。参考的九个 TypeScript 文件共 2,696 行。版权与授权见 [第三方声明](../../THIRD_PARTY_NOTICES.md)。

## 对应关系

| TypeScript | Go | 当前选择 |
| --- | --- | --- |
| `context.ts` | `context.go` | 显式方法与不可变 Scope 视图 |
| `registry.ts` | `plugin.go`、Context.Plugin、Fibers | 每次注册一个 Fiber，名称为诊断标签 |
| `fiber.ts` | `fiber.go`、`lifecycle.go`、`effect.go` | 串行协调、激活版本、取消、回滚与单次清理 |
| `reflect.ts` | `service.go` | 显式 Provide/Get、Key[T]/Resolve[T] |
| `service.ts` | 消费侧小接口与 Provider 插件 | 组合替代继承，没有基类 |
| `events.ts` | `events.go` | 五种分发模式、Next、显式 Filter |
| `logger.ts` | slog、WithLogger、Context.Logger | 标准库日志带插件字段 |
| `utils.ts` | 内部 effect、绑定与错误函数 | 保留需要的所有权行为 |
| `index.ts` | Go package cordis | Go 导出声明形成公共 API |

## 已实现的行为

插件实例、父子所有权、必需服务等待/失效/恢复、Provider 重启带动 Consumer 重启、按服务名隔离与相同 Scope 共享、依赖快照供清理读取、倒序释放、手动清理被所有者等待、错误与 panic 回滚、配置校验/更新/重启、Once/Prepend/Global/Filter、五种事件分发、诊断快照与命名日志。

## 有意采用的 Go 行为

- 服务通过函数和接口访问，代替动态属性 Proxy、装饰器和 declaration merging。
- Validate 显式接收并返回配置；初始配置在注册时校验，不等到激活。验证函数没有注入 Context 参数。
- 生命周期回调串行执行，独立清理按明确顺序执行；注入消费者先于 Provider 清理。
- Registry 按实例 ID 记录，不按 JavaScript function 身份合并 Runtime。
- Serial/Bail 使用同一同步实现；Parallel 使用 goroutine 并等待所有回调。
- Root.Close 永久结束容器；Root 不支持 restart。TypeScript 根 disposer 的可重启行为不在本版范围。
- 事件过滤为显式 Dispatcher 谓词，服务 Scope 不自动决定事件范围。

## 未实现

Context.extend 元数据、intercept 配置合并、Set/Accessor/Mixin、service availability check、traceable proxy、生成器/异步生成器 Effect、嵌套 Effect 诊断树、internal/* 拦截事件、内部状态通知、Standard Schema 适配、Volatile config、Loader/YAML include、Timer、模块 HMR 与客户端。

本版目标是可验证的生命周期与注入基础，并非 Cordis 4.0.4 的完整 API 兼容实现。下一层按 [路线图](../roadmap.md) 增加；涉及行为差异时同步维护本页及测试。
