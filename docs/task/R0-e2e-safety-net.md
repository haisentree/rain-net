# R0:e2e 安全网迁移

- 状态:done(2026-09-11)
- 依赖:无
- 关联:`docs/design.md` §八(R0)、§六(四场景即未来 e2e 用例蓝本)

## 目标

重构期间唯一的验收标准是"e2e 全绿"。本任务把现有测试迁入 `test/e2e/`,确立安全网,**不做任何行为变更**。

## 交付物

- `test/e2e/` 目录,承接以下测试(从 `internal/star/starserver/` 迁出):
  - `proxy_e2e_test.go`(反向代理全链路)
  - `tls_test.go`(TLS 通道)
  - `admin_api_test.go`(管理 API,含 connecttable 单测)
- `protocol/star/` 的单元测试**原地保留**(协议库测试不属于 e2e);
- 一键跑法确认:`go test ./...` 全绿。

## 迁移时的方案调整(执行记录)

- **`ctrl_proxy_test.go` 未迁移,保留在 starserver 包内**:其 `TestCtrlProxyCredentials` 依赖非导出方法 `ctrl.creds()`,且整体属单元测试粒度(流表、凭据、身份绑定);R3 拆 star 时随包结构归位;
- **`forward.go` 未使用 import 已修**(zplugin 的失败是被它连带的)——R1 清单中该项提前完成;
- **实验代码提前删除**:`go build ./...` 在基线提交上从未通过——`cmd/{net-test,test}` 引用不存在的包,`cmd/{mp,net2,trans}` 多个 main 冲突(历史上单文件 go run 使用)。按 §十.3 把挡路的五个实验目录提前删除;`cmd/{etcd,problem,custom,star,starclient}` 编译正常,保留待 R3。

## 验收结果

- [x] `go test ./...` 全绿,基线用例数不减少(30 → 30:protocol/star 12、protocol/ws 6、starserver 6、test/e2e 6);
- [x] 迁移后的测试不依赖特定工作目录(测试本就无路径依赖);
- [x] `go build ./...` 历史上首次全绿;
- [x] `docs/task/README.md` 状态表已更新。

## 进度日志

- 2026-09-11:完成迁移。git mv 三文件至 test/e2e,包名改 e2e,包内标识符加 starserver. 前缀,新增 helpers_test.go(waitFor 副本);修复 forward.go;删除五个损坏实验目录。分支 r0-e2e。
