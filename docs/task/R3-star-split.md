# R3:star 拆解 + 新配置 schema + admin 聚合

- 状态:in-progress(切片1-3 + 切片4a-4c 均已合入 main,2026-09-11)
- 依赖:R2
- 关联:`docs/design.md` §三(命名与目标 schema)、§五(迁移映射)、§九.2(半关帧);`docs/research/gost.md`;`docs/star-protocol.md`

## 目标

把 star 隧道从"特殊路径"拆进统一模型(handler/star + dialer/star + internal/client),切换目标配置 schema,admin API 改造为控制面聚合点。这是重构的最后一步,此后产品功能全部长在模型上。

## 交付物

- `internal/handler/star/`:隧道服务端协议(握手/注册),`StreamRegistry`(现 ctrl_hub + streamtable 合并定名);
- `internal/dialer/star/`:隧道出站(connOpen + 流适配为 rainnet.Conn,`starStreamAdapter`,CloseWrite → 半关帧);
- `internal/client/` + `cmd/rainc/`:客户端域(本地服务拨号、凭据、重连退避);
- `cmd/raind/`:服务端入口(register.go 空白导入装配);
- 配置:按 design.md §三目标 schema 重写(`ingress/dialers/routes/credentials`);**旧 schema 直接不兼容**——检测到旧字段即报错并输出新旧字段对照提示(§十.2,零兼容代码);
- star 协议变更:区分半关/全关(reserved 位标志或新增消息类型,§九.2),同步更新 `docs/star-protocol.md`;
- admin 改造:引入 `APIProvider`,现有 `/api/clients|streams|connects|kick|clientProxies` 平移到 `/api/tunnel/*` 命名空间,内嵌 Web 同步改路径;
- 清理:**删除** `cmd/{mp,net-test,net2,etcd,problem,test,custom,trans}` 实验代码(§十.3,git 历史保留可找回),`cmd/` 只留 raind/rainc;
- `pluginer/` 迁入 `pkg/pluginer/`;`internal/star/plugin/*` 迁入 `internal/handler/*`。

## 步骤

1. 先做 star 协议半关/全关帧变更(带协议单测),更新协议文档;
2. starStreamAdapter 适配新帧语义,conformance 增补 star 场景;
3. 拆 handler/star(StreamRegistry)与 dialer/star,proxy 链路改走 router→dialer;
4. 建 internal/client 与 cmd/rainc、cmd/raind;
5. 新 schema 落地:config 包仅识别新 schema,旧字段检测报错 + 新旧对照提示;
6. admin APIProvider 聚合改造 + web 路径同步;
7. 清理实验代码与目录迁移(§五 迁移映射表逐行核对);
8. e2e 用新 schema 重写配置部分,全绿。

## 验收标准

- [ ] design.md §六 场景 D(隧道星型)e2e 通过,链路上无"ctrlproxy/proxy 特殊类型"代码路径;
- [ ] 旧 schema 配置文件加载时明确报错,错误信息含新旧字段对照;
- [ ] 半关闭语义经 star 隧道端到端保持(CloseWrite 穿透到内网服务侧);
- [ ] `docs/star-protocol.md` 与实现一致;`go test ./...` 全绿。

## 进度日志

- 2026-09-11:切片2(隧道端到端半关闭)合入——CtrlClient 半关感知(半关帧→内网连接 CloseWrite、内网 EOF→半关帧),ProxyServer 生命周期重构为双方向独立收尾(与 R2 Bridge 同构);新增 e2e TestProxyHalfCloseEndToEnd(EOF 反应式后端穿透隧道)。切片3(StreamConn/StreamHub 适配器)合入——protocol/star/adapter.go,五场景测试。- 2026-09-11:切片4a-4c 合入——4a 配置模型类型抽至 config/ 包(starserver 别名引用,零改动);4b 二进制定名 cmd/raind + cmd/rainc,删除 cmd/{etcd,problem,custom};4c ctrl 三件套(ctrl_hub/streamtable/ctrlproxy)+ ctrl_proxy_test 迁入 internal/handler/star(包名 starhandler),ConnEntry 字段导出(Sink/Done/RemoteClosed/Stream)供 proxy 跨包使用,listenTCP 等价迁移。**下一步:切片4d = 新 schema(ingress/dialers/routes,§十.2 旧 schema 报错)+ admin APIProvider 聚合(/api/tunnel/*);切片4e = proxy/admin 迁入 handler 域收口 ConnEntry、dialer/star(基于 StreamHub)、internal/client(NewDialer 迁移)。**切片1 记录:——reserved 标志位 FlagHalfClose 区分 streamClose 半关/全关,IsHalfClose()/HalfClose(streamID) 辅助,旧对端降级兼容;协议测试覆盖全关/半关/未知标志透传;star-protocol.md 已同步。**下一步:切片2 = starStreamAdapter(用 CloseWrite→半关帧)+ conformance 增补 star 场景,然后按本文件交付物顺序推进拆包/schema/admin。**
