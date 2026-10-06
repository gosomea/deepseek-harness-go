# M11 Web 客户端与 Desktop 载体

## 阶段结果

让多个客户端连接 Go Host，以权威 Session 和投影恢复界面，并将 Desktop 作为独立载体验证。

本文是待实施方案；共同执行与门禁规则见[总索引](index.md)。

## 进入条件与范围

必需前驱：[M10](m10-protocols.md) 的 G10。

Go 实现 Host。前端保留 Web 技术栈，先用契约测试决定复用 DSH 客户端的范围；通过后再选定源码导入和构建方式。参考 DSH checkout 仍只读。

学习主题：C15，概念前置关系见[学习地图](../../../learning/index.md)。

## 固定源码入口

下面路径已经在固定 DSH 提交中核对存在；进入具体切片时继续定位符号与参考场景。源码链接不会追随本地未提交修改。

- [packages/client](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/client)
- [packages/host/webserver](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/host/webserver)
- [packages/host/frontend-static](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/packages/host/frontend-static)
- [apps/web](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/apps/web)
- [apps/desktop-host](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/apps/desktop-host)
- [apps/desktop](https://github.com/deepseek-ai/deepseek-harness/tree/00102833dfaee1da9f48a3a8eae9d34005a75218/apps/desktop)
- [docs/subsystems/web-client.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/subsystems/web-client.zh.md)
- [docs/subsystems/web-server.zh.md](https://github.com/deepseek-ai/deepseek-harness/blob/00102833dfaee1da9f48a3a8eae9d34005a75218/docs/subsystems/web-server.zh.md)

## 实现与学习切片

| 切片 | 必需前置 | 实现与预计产物 | 概念／学习交付 | 本切片验收 |
| --- | --- | --- | --- | --- |
| M11.1 | G10 | 制作客户端接入 spike，验证认证、资源加载、首屏 snapshot 和最小流 | 解释 Host 状态、客户端 store、实时 delta 的差别 | 复用可行性有可运行证据；不兼容点有明确适配边界 |
| M11.2 | M11.1 | 在本仓库建立 web 的构建、模块、resources/store 与基本 chat/session UI | C15 从页面操作追到 API 和 Session | 文本、工具结果、错误及 Session 切换正确；UI 不生成第二份权威日志 |
| M11.3 | M11.2、G8/G9 对应功能 | 逐项接入模型／配置、附件、审批、子代理、任务、交付物、终端 | 每项 UI 指向所属领域概念页 | 59 个 client 包有逐包处置与行为条目；缺后端能力的页面不虚假展示可用 |
| M11.4 | M11.3 | 实现断线恢复、快照增量衔接、多窗口和慢消费者处理 | 断开网络后预测重新连接的消息与工具卡片 | 无缺失／重复；审批回答归属正确；订阅和页面卸载释放 |
| M11.5 | M11.4 | 建立 Desktop Host 启动桥、私有 profile、认证注入和进程关闭 | 区分 browser HTTP 与 Desktop 载体通道 | 就绪／致命失败／关闭／重启有证据；不得假设 browser server 等于 Desktop IPC |
| M11.6 | M11.5 | 加入浏览器 E2E、可访问性、截图与桌面 smoke；整理用户操作路径 | C15 从 UI 状态回查持久事实与实时事件 | 前后端锁文件与构建可复现；平台测试和未验范围准确 |

## 修改范围与交付文件

以下为预计修改范围，具体 inputs/outputs 在进入阶段时登记。

- `web/`
- `desktop/`
- `plugins/hostapi/`
- `app/`
- `docs/client/`
- `docs/desktop/`
- `testdata/parity/client/`

## 读者实验

两个浏览器窗口连接同一 fake Session，一个窗口掉线，另一个继续执行工具并回答审批；重连后比较权威 snapshot 与 UI，确认不重复执行工具。

## 行为与失败检查

- 加载成功但 Host 未就绪，UI 展示真实等待与错误。
- 实时 delta 到达而对应 settlement 尚未写入，刷新后的表现符合契约。
- 订阅断开期间产生工具结果与审批取消，重连不能复活旧请求。
- 窗口关闭／Desktop 退出后 Host、PTY、流监听器都释放。
- 界面支持新内容块而 Provider 或持久重放不支持，功能不能提前开放。

## 计划中的检查

这些命令待实现落地后执行；本次细化不运行未来门禁。专用 fixture、边界与平台检查须在进入阶段时补齐。

```sh
go test -race -count=1 -timeout=60s ./plugins/hostapi/... ./app/...
make check
```

## 退出门禁与交接

**G11：选定前端技术栈与锁文件；浏览器构建、E2E、类型检查、可访问性和 Desktop smoke 均登记进子计划并实际执行；C15 路径可操作。**

M12 负责三平台桌面打包、升级、签名／发布与剩余 UI 全量收口。未验对应后端功能时不能把该 UI 行为计入完成。

## 阶段内要冻结的选择

本页 Go 命令不覆盖 UI。M11.1 必须确定 web/desktop 工具链和精确命令，再登记执行契约；在此之前 G11 不具备可执行的完整检查集合。先验证复用，不提前承诺所有 TS 插件可原样加载。
