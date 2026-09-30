# 贡献指南

## 开始修改

先从 [文档索引](docs/README.md) 找到模块行为说明，读取所属包的 README。新模块先定义职责、依赖、注册与释放方式，再写 Provider 和 Consumer。较大工作把执行计划放入 `docs/10-plans/`，沿用现有阶段依赖。

## 完成一个修改

1. 修改实现与受影响调用方；接口当前处于学习项目的早期阶段，改动时同步更新所有消费者。
2. 增加行为测试，覆盖资源所有权、失败路径和用户可观察的结果。
3. 按[文档规范](docs/documentation.md)更新分类 README 和行为文档，补充公开 Go 注释；完整程序使用可运行标记并给出输出。公开接口变化后执行 `make api`。
4. 执行 `make check`，在阶段证据中记录命令、工具版本、结果和未验证环境。

详细命令见 [开发说明](docs/development.md)；测试种类与门禁规则见 [测试文档](docs/testing.md)。

## 提交与评审

推荐把后续阶段拆为可运行的小修改：Context/服务 → 生命周期 → 事件 → Loader → Session。代码与对应文档一起提交。先解决影响当前阶段的错误，再推进依赖它的阶段。GitHub 推送会触发 [CI](https://github.com/gosomea/deepseek-harness-go/actions)。提交前运行本地门禁；推送后核对对应提交的 CI 结果。
