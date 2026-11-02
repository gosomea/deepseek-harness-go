# M12.5 实验能力缺口

本切片在阶段文档中要求逐类补浏览器、计算机操作、Office、语音、Agent Teams、WebWorker 等实验能力。**本环境无法交付真实实现**，逐项记录为缺口（不伪造成通过）。

| 能力 | 状态 | 理由 |
| --- | --- | --- |
| 浏览器自动化（browser-use） | gap | 需真实浏览器驱动与隔离载体，未实现 |
| 计算机操作（computer-use） | gap | 需真实桌面控制与权限，未实现 |
| Office 转换（document） | gap | 需 LibreOffice 等外部引擎，未集成 |
| 语音 | gap | 需 TTS/ASR 供应商，未集成 |
| Agent Teams | gap | 属实验性多代理产品面，未实现 |
| WebWorker | gap | 依赖 JS 运行时，Go 静态二进制无等价物 |

以上在账本 `dispositions.json` 中对应 `browser-use`/`computer-use`/`document`/`experimental` 组，状态 `gap`，由 `cmd/conformance` 校验。
