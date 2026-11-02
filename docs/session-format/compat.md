# M12.3 Session 格式兼容缺口

本切片要求实现 DSH Session 格式读取/迁移/generation 发布与完整 storage/query Provider。**本环境未实现**。

| 能力 | 状态 | 理由 |
| --- | --- | --- |
| 自有 JSON Lines 格式（M6） | 已做 | 帧、lease、flush、损坏恢复 |
| 原 DSH Session 格式读取 | 未实现 | 需固定版本 golden fixtures |
| 格式迁移链/generation 发布 | 未实现 | 未设计 |
| storage/query Provider（session-query 组） | gap | 未实现索引与查询 |
| 跨实现互读 | 未验证 | 无参考实现离线产物 |

结论：能处理 JSONL **不等于**兼容 DSH Session；不得声称可读取原 DSH 会话文件。
