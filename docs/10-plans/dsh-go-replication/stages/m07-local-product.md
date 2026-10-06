# M7 真实模型、本机执行策略与 CLI

## 阶段结果

在临时工作区通过 CLI 使用真实模型与本机工具，恢复 Session，并证明所有副作用先经过明确策略。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

必需前驱：[M6](m06-persistence.md) 的 G6。

接通 tRPC 适配器的凭据／真实端点，以及本地 workspace、fs、subprocess、shell、审批和 CLI。先完成策略与执行能力，再暴露写文件和命令工具。

学习主题：C06 的真实接入、C11，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [packages/credentials](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/credentials)
- [packages/workspace](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/workspace)
- [packages/fs](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/fs)
- [packages/subprocess](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/subprocess)
- [packages/shell](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/shell)
- [packages/sandbox](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/sandbox)
- [packages/interaction/user-approval](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/interaction/user-approval)
- [packages/interaction/permission-presets](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/interaction/permission-presets)
- [apps/cli](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/apps/cli)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M7.1 | G6 | 定义 capability 的 workspace/fs/subprocess/sandbox/approval 接口、错误和平台能力 | 用同一 cwd 解释文件与进程的执行世界 | 运行配置所需能力缺失时拒绝启动；不能隐式降级隔离 |
| M7.2 | M7.1 | 实现审批请求／一次性结果、headless 政策和不可放宽 guard | 预测无人应答和取消审批的结果 | unavailable/rejected/cancelled 均不授权；绑定具体调用身份 |
| M7.3 | M7.2 | 实现本地 fs／subprocess Provider、路径规则、进程树取消与输出限额 | 实验让进程产生超限输出再取消 | 越界路径、符号链接、cwd、权限与子进程释放有平台证据 |
| M7.4 | M7.3 | 注册读取／编辑／shell 工具，接入观察／写入前置条件和 sandbox | 修改一个被外部改变的文件，解释冲突检测 | 工具只经能力接口；先过策略再写；冲突不会静默覆盖 |
| M7.5 | G6 | 接入凭据服务、配置优先级与 tRPC 真端点；验证脱敏与重试所有者 | 从回环请求切到真实配置，解释凭据边界 | 日志不输出密钥；路由/参数错误可诊断；真实 smoke 单列 |
| M7.6 | M7.4、M7.5 | 补 cmd/dsh-go 的输入、stdout/stderr、退出码、信号、会话恢复和 profile | C11 完成一次读文件→受控修改→退出→恢复 | 终端退出等待 owned 资源；非交互审批政策确定；CLI 仍走唯一 app |
| M7.7 | M7.6 | 以 fake 模型执行无密钥 CLI E2E，另运行有凭据的 smoke | 记录真实调用与假数据测试能证明的不同范围 | Linux/macOS/Windows 支持矩阵完整；不支持项明确阻塞相应配置 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `capability/workspace/`
- `capability/fs/`
- `capability/subprocess/`
- `capability/sandbox/`
- `capability/approval/`
- `plugins/local/`
- `plugins/llmtrpc/`
- `app/`
- `cmd/dsh-go/`
- `docs/execution/`
- `docs/cli/`

## 读者实验

用 fake 模型请求在临时目录写文件，第一次拒绝审批，第二次允许一次，第三次换路径重新询问；随后重启 CLI 恢复历史。真实模型演示使用同一条管线。

## 行为与失败检查

- 无审批 responder 的 ask、重复回答、迟到回答与取消。
- 读取路径与执行 cwd 不属于同一世界，配置被拒绝。
- 命令退出非零、超时、输出截断、父进程关闭仍有子进程。
- 凭据缺失、无效端点、网络断开以及 response 内错误。
- Windows shell 与路径规则单独验证，不以 Bash 测试代替。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
go test -race -count=1 -timeout=60s ./capability/... ./plugins/local/... ./plugins/llmtrpc/... ./cmd/dsh-go/...
make check
```

## 退出门禁与交接

**G7：keyless CLI E2E、策略、进程与文件测试、恢复和平台矩阵通过；真实模型 smoke 状态另记；C11 实验及限制完整。**

向 M8 提供受控文件／引用访问，向 M9 提供 owned 执行资源，向 M10 提供可由远程 Provider 替换的能力契约。

## 阶段内要冻结的选择

API token 来源、默认权限和受支持沙箱在实现前固定。账户平台登录、完整原生能力、持久终端与全部 shell Provider 继续在 M10/M12 分配，不用简单命令执行冒充它们。
