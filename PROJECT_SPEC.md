# 古生物修复制备台项目说明

## 项目目标

本项目为古生物标本制备组提供一个可运行的后端基线，用于管理标本登记、修复方案审批、制备进度和研究成果放行。系统关注的是标本状态的真实流转、同一方案下的连续记录以及放行前的完整性约束。

## 使用方

- 登记员：录入标本目录号、野外编号、基质类型和临时保护信息。
- 制备负责人：为标本拟定修复目标和方法，并作出审批决定。
- 制备人员：按阶段提交基质去除比例、操作说明和记录人。
- 标本管理员：核对已完成制备的标本并办理研究借阅放行。

## 核心实体

- 标本：保存目录号、野外标签、分类提示、基质类型、夹克备注、状态和版本号。
- 修复方案：绑定一份标本，保存目标、方法、黏结材料、目标暴露比例、审批信息和状态。
- 进度记录：属于一个修复方案，按序号保存阶段名称、完成比例、说明和记录人。
- 放行记录：绑定已稳定的标本，保存借阅目的、接收方和批准人。
- 证据记录：关联标本、方案或某条进度序号，保存类型、内容摘要、提交人和提交时间，登记后不可修改或删除。
- 证据封存：方案完成或放行成功时在同一事务中生成，固定当时读取到的证据关系和内容摘要指纹。

## 状态与规则

标本状态依次经过 `registered`、`planned`、`preparing`、`stabilized` 和 `available`。同一目录号不能重复登记。一个标本同时最多存在一个未结束的修复方案。

修复方案状态依次经过 `draft`、`approved`、`active` 和 `completed`，草稿也可以进入 `cancelled`。只有草稿能够审批；只有已审批方案能够开始记录进度。

进度比例必须位于零到一百之间，且新的比例不得低于上一条记录。比例达到一百时形成最后一条记录，方案进入 `completed`，标本进入 `stabilized`。完成后的方案不能再写入进度。

放行只允许发生在标本进入 `stabilized` 且不存在未结束方案时。放行成功后标本进入 `available`，并永久保留一条放行记录。

证据类型限定为 `photo_before`、`photo_after`、`measurement` 和 `material_batch`；内容摘要必须是 64 位十六进制 SHA-256，用于校验文件是否被替换。同一份内容摘要全库只能登记一次，重复提交返回冲突。方案完成时把方案级和进度级证据写入方案完成封存；放行时把标本全部证据写入放行封存。封存生成后，针对同一对象的新登记返回冲突，已封存的证据关系永久不变。

## 业务链路

### 登记标本

登记员通过 `POST /specimens` 提交目录号、野外标签、分类提示、基质类型和夹克备注。服务校验必填字段和目录号唯一性，然后创建 `registered` 标本。`GET /specimens/{id}` 可读取完整快照。

### 审批修复方案

负责人通过 `POST /specimens/{id}/plans` 创建草稿方案，再通过 `POST /plans/{id}/approval` 写入批准人和审批时间。每一步都检查标本状态和未结束方案约束，审批成功后标本进入 `preparing`。

### 记录制备进度

制备人员通过 `POST /plans/{id}/progress` 追加阶段记录。第一条有效进度会把方案从 `approved` 推进到 `active`；比例达到一百时方案和标本在同一仓储事务中进入终态，防止只更新其中一方。

### 办理研究放行

标本管理员通过 `POST /specimens/{id}/release` 提交借阅目的、接收方和批准人。服务再次检查标本状态和方案闭合状态，成功后写入不可覆盖的放行记录并把标本置为 `available`。`GET /specimens/{id}/release` 可读取放行记录和放行时封存的证据关系。

### 登记现场证据

操作者通过 `POST /specimens/{id}/evidence`、`POST /plans/{id}/evidence` 或 `POST /plans/{id}/progress/{seq}/evidence` 把修复前后照片、测量记录或材料批次凭证关联到标本、方案或某个进度序号，并提交类型、内容摘要和提交人。关联对象不存在返回 `404`，类型或摘要格式不合法返回 `400`，重复摘要返回 `409`，方案完成或放行后的新登记返回 `409`。`GET /evidence/{id}` 读取单条证据，`POST /evidence/{id}/verification` 比对当前文件摘要并返回 `match` 或 `mismatch`，用于判断文件是否被替换。`GET /plans/{id}` 与 `GET /specimens/{id}/release` 返回各自封存的证据关系和清单指纹。

## 模块边界

- `cmd/paleo-prep-station` 负责进程参数、监听和关闭。
- `cmd/workflowcheck` 负责通过公共 HTTP 路由执行独立的本地链路检查。
- `internal/httpapi` 负责传输层解码、输出和错误状态映射。
- `internal/station` 负责加载对象、执行业务条件并提交原子变更。
- `internal/domain` 负责实体构造、状态转换和不变量。
- `internal/repository` 负责互斥保护和事务副本提交。
- `internal/contracts` 负责公共请求和稳定错误类型。

## 接口约定

- `POST /specimens` 成功返回 `201` 和标本对象。
- `POST /specimens/{id}/plans` 成功返回 `201` 和方案对象。
- `POST /plans/{id}/approval` 成功返回 `200` 和更新后的方案。
- `POST /plans/{id}/progress` 成功返回 `201` 和进度记录。
- `POST /specimens/{id}/release` 成功返回 `201` 和放行记录及证据封存。
- `POST /specimens/{id}/evidence`、`POST /plans/{id}/evidence`、`POST /plans/{id}/progress/{seq}/evidence` 成功返回 `201` 和证据记录。
- `GET /specimens/{id}/evidence` 成功返回 `200` 和标本全部证据；`GET /evidence/{id}` 返回 `200` 和单条证据。
- `POST /evidence/{id}/verification` 成功返回 `200` 和摘要比对结论。
- `GET /specimens/{id}/release` 成功返回 `200` 和放行记录及证据封存。
- 输入不合法返回 `400`，对象不存在返回 `404`，状态冲突或重复标识返回 `409`。

## 验证计划

构建命令为 `go build ./...`。当前基线不生成自动化测试文件，使用五条生产级 HTTP 链路检查覆盖主要成功路径和关键冲突边界。后续任务阶段再补充红绿验证测试，并保留这些检查作为端到端烟雾路径。
