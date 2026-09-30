# Go 学习文档修订的本机验证

客户端日期：2026-09-30。Go 1.22.12，darwin/amd64，CGO_ENABLED=1。必要检查由计划 CLI 实际执行；[完整输出](make-check.stdout)与 [stderr](make-check.stderr)保留本机结果。[report.md](report.md)是验收入口，[probes.json](probes.json)记录顶层拒绝与教程运行验证。

文档和示例检查、完整 make check 及 diff 检查通过；Cordis 语句覆盖率 97.7%。四个完整 Markdown 程序编译并执行；三个教程按 bin/tutorial.go 的操作路径运行，缺少依赖的练习保持 Pending。顶层工具拒绝空使用节、类型错误和错误输出；测试另覆盖运行失败、分类、元数据等路径。

语义审阅由同一代理单独完成，independent=false；核对了 Plugin/Fiber/激活区分、Context 与标准库取消、资源所有权、并发限制、示例前置条件和来源链接。原 DSH checkout 只读，冻结验证导出不修改；旧 Cordis 状态按当前产物重新推导，本次交付状态由新计划维护。

本机证据不代表远端 CI。推送后查询同一提交的 GitHub Actions 三平台结果；CI 运行链接由交付说明提供，仓库的测试文档保留查询入口。
