# M2 Loader、配置身份与最小 Profile

## 阶段结果

用一份配置装配出稳定、可更新的插件树，让读者区分配置条目、Fiber 与激活。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

必需前驱：[M1](m01-cordis.md) 的 G1。

交付已编译插件目录、JSON 配置、稳定 Entry ID、树更新、最小 profile 与统一装配入口。原 YAML/profile/npm 插件兼容另有条目，不能由本阶段默认宣称。

学习主题：C05，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [vendor/loader](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/loader)
- [vendor/include](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/include)
- [vendor/group](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/vendor/group)
- [packages/boot/app-boot](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/boot/app-boot)
- [packages/bundle](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/bundle)
- [apps/cli](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/apps/cli)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M2.1 | G1 | 在 loader 定义 Entry、工厂目录与配置 codec；区分原始配置和校验结果 | docs/loader/go-primer.md 解释 struct 配置、工厂与运行时身份 | 未知名称、重复 ID、错误字段、禁用配置有明确结果 |
| M2.2 | M2.1 | 装配父子树与分组，绑定 Entry→Fiber，提供 dump/诊断 | 用同一个配置观察声明树和运行树 | 嵌套定位与所有者一致，关闭根树释放全部条目 |
| M2.3 | M2.2 | 实现按 ID 增删改、启停与移动；定义等待和部分失败 | 实验比较修改普通配置、禁用、删除的结果 | 失败保留哪些条目明确；不误承诺整树回滚；更新不无故改变身份 |
| M2.4 | M2.3 | 在 app 定义最小 bundle/profile、按序 overlay 和 required 规则 | 解释配置来源优先级和必需项失败 | 替换配置的语义有 fixture；必要条目失败阻止 ready，兄弟项策略可观察 |
| M2.5 | M2.4 | 建立 cmd/dsh-go 的配置检查／dump 入口和 examples/loader | 完整 JSON 示例、错误修复、TS Entry 源码对照 | 同一启动装配路径可用于测试与 CLI；输出确定、关闭无遗留 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `loader/`
- `app/`
- `cmd/dsh-go/`
- `examples/loader/`
- `testdata/parity/loader/`
- `docs/loader/`

## 读者实验

先配置 Consumer 和 Provider，打印配置树与 Fiber 状态；按 ID 更新 Provider，再禁用它，观察 Consumer 等待；恢复后校验 ID 与资源数。

## 行为与失败检查

- JSON 解析失败发生在应用更新前，不能破坏当前运行树。
- 配置可解析但某插件激活失败：按已核对的非事务语义报告实际状态。
- 同名插件不同 ID、重复 ID、跨组移动分别有规则。
- 依赖未满足与配置无效必须产生不同诊断。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
go test -race -count=1 -timeout=60s ./loader/... ./app/... ./cmd/dsh-go/...
go run ./examples/loader
make check
```

## 退出门禁与交接

**G2：正常加载、更新、分组、启停、失败与关闭全部有 fixture；最小 profile 可复现；C05 实验与源码对应完整。**

M5 通过 app 装配 fake Harness；M7 将 CLI 输入和真实 Provider 加到同一个入口。YAML/include/惰性表达式/配置 HMR/原 profile 兼容列为 M12.2 必验项。

## 阶段内要冻结的选择

JSON 是首个输入形式；patch 替换／合并、默认值、惰性注入和 disabled 的语义必须逐项说明。临时静态注册不会被宣传为 npm 动态安装兼容。
