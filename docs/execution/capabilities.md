---
description: "为什么 Agent 需要一个受控的执行世界：同一个 cwd 解释文件与进程，能力缺失时拒绝而不是降级。"
kind: "concept-note"
---

# 能力与执行世界：同一个 cwd 解释文件与进程

## 读完要解决的问题

一次 Agent 轮次可能要读文件、改文件、跑命令。这三件事看起来只是"调用不同的 API"，但真正要先确定的是**它们发生在哪个世界里**：

1. **读取路径与执行 cwd 是同一个世界吗？** 如果工具读的是 `/work/a`，而命令的 cwd 是 `/elsewhere`，那么模型看到的和实际执行的就对不上。
2. **路径"在工作区内"是可判定的吗？** `/work` 与 `/workshop` 前缀相同；符号链接可能指到外面。
3. **平台给不了所需限制时怎么办？** 是"尽力而为地跑"，还是拒绝启动？

读完后你能：解释为什么工作区身份是生成值而不是路径；说明为什么包含判断要逐元素比较；在 `capability/` 与 `plugins/local/` 里找到每条规则的实现与对应测试。

需要的 Go 知识：接口与方法集、`context` 取消、`errors.Is`、`filepath` 的清理与相对路径。

## 从熟悉的 Go 写法出发

普通的 Go 工具直接拿路径就能用：

```go fragment
content, err := os.ReadFile(path)
if err != nil {
	return err
}
```

在"调用方自己保证路径安全"的前提下这是对的。它的边界有三处：

- **没有世界**：路径是绝对的还是相对的、相对于谁，全靠约定。
- **没有包含判断**：字符串前缀比较会把 `/workshop` 当成 `/work` 的子目录。
- **没有能力协商**：平台给不了隔离时，代码只会继续跑。

DSH 侧的概念对应是「能力 seam + 工作区身份」：

- **seam（接缝）**：可替换的边界接口。`capability/fs`、`capability/subprocess`、`capability/sandbox`、`capability/approval` 各定义一个。
- **工作区（workspace）**：一个目录上的**稳定身份**，加上让"在里面"可判定的规范化规则。
- **能力（capability）**：Provider 能承诺什么，**报告**而不是假定。

## 最小实验

准备：仓库根、无需密钥、无需网络。

```sh
go test -race -count=1 ./capability/... ./plugins/local/...
```

预期：全部 `ok`，退出码 0。

最小的成功场景是"世界内的路径被接受、世界外的被拒绝"：

```sh
go test -count=1 -run 'TestResolveRejectsSiblingWithSharedPrefix|TestPathsOutsideTheWorldAreRefused' -v ./capability/workspace/ ./plugins/local/
```

预期两个测试 PASS：第一个断言 `/work` 与 `/workshop` 不是包含关系；第二个断言一个 `..` 路径既不被读、也不被写。

再增加一个因素：符号链接逃逸。

```sh
go test -count=1 -run 'Symlink' -v ./capability/workspace/ ./plugins/local/
```

预期 PASS：世界内指向外部的链接在解析开启时被拒绝；创建路径经由"符号链接父目录 + 不存在的叶子"也被挡住。

## 先预测，再观察

**预测一**：开启符号链接解析后，如果**根**本身是经由链接抵达的，世界内的路径会被判为在里面还是外面？

观察：`TestWorldCanonicalizesItsRootWhenResolving`。结果：会被判为**在外部**——除非根也一并规范化。macOS 上临时目录经由 `/var` → `/private/var`，所以这不只是理论问题：它让 `plugins/local` 的写入测试最初就失败。修法是构造时也规范化根。恢复方式就是这条测试。

**预测二**：创建一个"符号链接父目录 + 不存在的叶子"的路径，整体解析会成功吗？

结果：不会——叶子还不存在，无法整体解析。所以 `ResolveForCreate` 向上找**最深的已存在祖先**、解析它、再拼回缺失尾部；这样经由链接父目录的逃逸仍然被挡住。

**预测三**：平台不支持沙箱，但配置要求沙箱。应该"尽力跑"还是拒绝？

结果：拒绝。`sandbox.Policy.Validate` 在生效模式宽于可用模式时返回 `ErrUnavailable`；`subprocess.Capabilities.Supports` 对缺失的进程树终止或沙箱返回 `ErrUnsupported`。理由与版本拒绝同源：**静默降级会让调用方以为拿到了从未存在的限制**。

## 两种实现怎样对应

| 项目 | 内容 |
| --- | --- |
| DSH 概念 | 能力 seam：workspace 身份与路径规则、fs/subprocess 受限实现、sandbox 模式与升级、approval 一次性授权 |
| TypeScript 入口 | 固定提交 `00102833` 的 `packages/workspace/workspace/src/{types,paths,spec}.ts`、`packages/fs/fs/src/types.ts`、`packages/subprocess/subprocess/src/{types,control}.ts`、`packages/sandbox/sandbox/src/{index,roots,escalation}.ts` |
| Go 入口 | [capability/workspace](../../capability/workspace/README.md)、[capability/fs](../../capability/fs/README.md)、[capability/subprocess](../../capability/subprocess/README.md)、[capability/sandbox](../../capability/sandbox/README.md)、[capability/approval](../../capability/approval/README.md)；实现见 [plugins/local](../../plugins/local/README.md) |
| 保持的行为 | 路径必须属于同一个世界；越界与链接逃逸被拒绝；能力缺失时拒绝启动；升级只能更宽 |
| Go 表达 | 消费者定义的小接口；`Workspace` 是值类型 + 接口；错误用 sentinel 分类；`context` 贯穿取消 |
| 不等价边界 | **不实现真实沙箱**（`Sandboxed` 只是报告字段）；Windows 树终止路径未验证；参考的 `roots.ts` 多根与 `diagnostics.ts` 未实现；账户平台登录与持久终端属后续里程碑 |

## 失败与定位

| 现象 | 原因 | 定位方式 |
| --- | --- | --- |
| 世界内路径被判为在外部 | 根未规范化而候选路径被解析 | 看 `workspace.New` 是否在开启解析时也解析根；`TestWorldCanonicalizesItsRootWhenResolving` |
| 兄弟目录被当成子目录 | 用了字符串前缀比较 | `contains` 用 `filepath.Rel` 判元素；`TestResolveRejectsSiblingWithSharedPrefix` |
| 创建经由链接父目录逃逸 | 只解析完整路径，叶子不存在就放弃 | `ResolveForCreate` 解析最深已存在祖先；`TestResolveForCreateFollowsExistingAncestors` |
| 平台无隔离却仍在跑 | 能力未被检查 | `Policy.Validate` 与 `Capabilities.Supports`；`TestCapabilitiesRefuseMissingFeatures` |
| 命令继承了本进程的凭据 | nil 环境被当成继承 | `plugins/local` 把 nil 变成空；`TestNilEnvironmentIsEmptyNotInherited` |

## 回到源码与下一步

- [capability](../../capability/README.md)：五个 seam 的定义。
- [plugins/local](../../plugins/local/README.md)：本机实现与工具。
- [审批与沙箱升级](approval.md)：下一步读这个，理解"谁能批准更宽的模式"。
- [平台矩阵](platform-matrix.md)：各平台的证据状态与未验项。
