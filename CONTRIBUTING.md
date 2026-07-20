# 参与开发

提交变更前请运行：

```bash
go test ./...
cd web && npm run test && npm run build
```

运行中的本地服务可额外执行 `cd web && npm run test:e2e`，端到端测试默认使用系统 Chrome 和 `http://127.0.0.1:18080`。

不要在测试、截图、日志或 issue 中提交真实的 B 站 Cookie、Bark Device Key、用户数据或加密密钥。涉及 B 站接口的测试必须使用本地模拟服务器。

提交信息建议使用简短的祈使句，并让每个提交只处理一个明确主题。新增行为应包含对应测试，同时更新 `CHANGELOG.md` 的 `Unreleased` 部分。
