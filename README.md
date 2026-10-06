# 古生物修复制备台

古生物修复制备台是一个使用 Go 编写的后端基线，用于记录化石标本、审批修复方案、回填制备进度，并在全部工序完成后办理研究借阅放行。

该服务只使用进程内仓储，便于在本机重复启动和检查。它不依赖数据库、远端接口或其他在线资源。

## 目录

- `cmd/paleo-prep-station`：HTTP 服务入口和监听参数。
- `cmd/workflowcheck`：通过真实 HTTP 处理器执行四条业务链路的本地验证命令。
- `internal/httpapi`：路由、请求解码、状态码映射和 JSON 输出。
- `internal/station`：跨领域对象的编排、事务边界和业务流程。
- `internal/domain`：标本、方案、进度和放行记录的状态与不变量。
- `internal/repository`：互斥保护的内存仓储和原子提交。
- `internal/contracts`：稳定错误模型和公共请求结构。

## 构建与运行

```bash
go build ./...
go run ./cmd/paleo-prep-station --addr 127.0.0.1:18133
```

服务启动后，`GET /healthz` 返回就绪状态。

## 主要接口

- `GET /specimens` 与 `POST /specimens`
- `GET /specimens/{id}`
- `POST /specimens/{id}/plans`
- `GET /plans/{id}`
- `POST /plans/{id}/approval`
- `POST /plans/{id}/progress`
- `POST /specimens/{id}/release`
- `GET /specimens/{id}/release`
- `POST /evidence`、`GET /evidence/{id}`
- `GET /specimens/{id}/evidence` 与 `GET /plans/{id}/evidence`

## 证据登记与固定

证据可以关联到标本、修复方案或某个进度序号，提交时需要给出受控类型（照片、测量记录、材料批次凭证等）、内容摘要、内容 SHA-256 校验值和提交人。系统在事务内校验关联对象真实存在、方案归属标本、进度序号已产生；同一关联坐标上重复的内容校验或摘要会被拒绝，并发提交只有一条成功。

证据只追加，不能修改或删除。方案完成时固定方案级和全部进度序号上的证据；办理放行时固定标本级、标本全部方案及全部进度序号上的证据。固定清单复制当时的证据并生成规范化摘要，之后新增证据不会改变历史固定结果。

## 本地验证

每条业务链路都可以独立执行：

```bash
go run ./cmd/workflowcheck --workflow register-specimen
go run ./cmd/workflowcheck --workflow approve-prep-plan
go run ./cmd/workflowcheck --workflow log-preparation
go run ./cmd/workflowcheck --workflow release-for-study
go run ./cmd/workflowcheck --workflow register-evidence
```

这些检查会启动临时 HTTP 处理器并调用真实公共路由。

## 环境变量

项目不需要环境变量，也没有外部服务依赖。

## 测试边界

本基线按要求暂不生成单元测试、测试夹具或端到端测试文件。后续任务阶段会补充红绿验证测试，届时再替换或扩展当前的生产级链路检查。
