# 版本与范围

- 模块：`github.com/gosomea/deepseek-harness-go`
- 参考基线：DSH 提交 `00102833dfaee1da9f48a3a8eae9d34005a75218`
- 覆盖到的里程碑：M0–M12（本仓库达到的执行契约状态以各 `docs/10-plans/dsh-go-replication-mNN/status.md` 为准）

## 范围声明

本版本**不是**完整 DSH 复刻。已验证范围：Cordis 运行时、装配、会话、模型与工具管线、持久化、本机执行、上下文与压缩、子代理与持久任务、协议与远程 seam（keyless）、客户端投影与 Desktop Host 侧。

明确缺口：真实 Web UI／E2E／Desktop 打包、三平台发行与签名、Python SDK、原 DSH 配置与 Session 格式兼容、PTC 生产 Provider、部分实验能力（浏览器／计算机操作／Office／语音等）。详见 [发行缺口](gaps.md) 与 [兼容缺口](../compatibility/gaps.md)。
