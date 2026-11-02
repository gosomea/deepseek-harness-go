# 兼容与配置缺口

本文件如实记录 M12 中**未验证的兼容性缺口**。未验证 ≠ 通过。

## 未验证缺口

- **YAML／include／惰性配置（`!!js`）／volatile／HMR**：M2 交付 JSON 最小配置；YAML include、惰性求值与配置热重载**未实现**，原 DSH profile 兼容未验证。
- **原始 DSH Session 格式读取／迁移／generation 发布**：M6 交付自有 JSON Lines 格式；**不兼容**原 DSH 会话文件，跨实现互读未验证。
- **storage／session-query Provider**（`session-query` 组）：未实现，属 gap。
- **PTC 执行 Provider 与生产 workflow 引擎**（`ptc-runtime` 组）：M9 交付 workflow seam 与 fake 引擎；生产 PTC Provider **未实现**，原脚本语义兼容未验证。
- **外部插件安装与进程隔离替代**：未实现，动态 JS 模块加载在 Go 静态二进制下无等价物。
- **Typert 代码生成**（`typert` 组）：wire 契约以手写 DTO 验证，typert 生成机制未复刻。
- **webhook**（`webhook` 组）：未实现。

这些缺口在能力表中保留，收口前不得据本阶段通过声称「原配置／原 Session／原插件可直接复用」。
