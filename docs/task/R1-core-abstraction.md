# R1:核心抽象定型(= roadmap M1)

- 状态:done(2026-09-11)
- 依赖:R0
- 关联:`docs/design.md` §二(模型)、§三(命名)、§四(目录)、§九.1(conformance)、§九.4(Meta.ID)

## 目标

把模型的通用语言与传输维度落成代码:pkg/rainnet、internal/transport(接口 + Registry + tcp/tls),装配从硬编码 switch 改为查表。纯重构,行为不变。

## 交付物

- `pkg/rainnet/`:`Conn` 接口(`net.Conn` + `CloseWrite() error` + `Meta()` + `SetTarget()`)、`Meta` 结构(含 `ID` 字段,§九.4)、`netConnAdapter` 适配器;
- `internal/transport/`:`Transport` 接口 + `Registry[T]` + `tcp/`、`tls/` 两个实现;
- `internal/star/starserver/register.go` 的 `switch listener.Transport` 硬编码改为 Registry 查表;
- 修复 `internal/star/plugin/forward`、`internal/star/zplugin` 编译错误(未使用 import)——**R0 已提前完成**;
- `test/conformance/`:传输一致性套件,echo / 大流量 / 半关闭排空 / 多流并发 / deadline / ctx 取消六类场景,tcp/tls 全部通过;
- Go module 路径改为 `github.com/haisentree/rain-net`(design.md §十.1,动工前全局替换);
- GitHub Actions CI:`go vet` + `go test ./...`(design.md §十.5)。

## 步骤

1. 建 `pkg/rainnet`(接口 + netConnAdapter;starStreamAdapter 留给 R3);
2. 建 `internal/transport` 接口与 Registry,迁移现 `tls.go`/tcp 逻辑为两个实现;
3. register.go 查表化(行为不变,靠 R0 e2e 验证);
4. 写 `test/conformance`,先对 tcp/tls 跑通;
5. 更新 design.md 若接口签名有微调。(原"修两个编译错误"已由 R0 完成)

## 验收结果

- [x] `go test ./...` 全绿(e2e 基线 30 用例不回退,conformance 新增 12 个子场景);
- [x] 新增一种 transport 只需"一个目录 + init 注册",zplugin 加一行空白导入,register.go/装配层零改动;
- [x] conformance 套件对 tcp/tls 全过(并在首次运行就逮住 Listen ctx 契约缺口,催生 transport.WatchClose);
- [x] 接口总数 ≤ 6(rainnet.Conn、transport.Transport,pluginer Handler 沿用),无新发明接口。

## 进度日志

- 2026-09-11:完成。module 路径全局替换 → github.com/haisentree/rain-net;pkg/rainnet(Conn/Meta/Wrap/适配器);internal/transport(接口+Registry+BuildFunc+WatchClose)+ tcp/tls 实现;register.go 查表化(udp 保留 PacketConn 特例;未知传输由 panic 改为启动报错,行为微调已记录);test/conformance 六场景套件;GitHub Actions CI(vet+test+gofmt)。接口微调已回写 design.md §二。分支 r1-core。
