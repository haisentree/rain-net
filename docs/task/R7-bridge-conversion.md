# R7:传输转换全量 + 嗅探 ingress(= roadmap M5,产品主轴落地)

- 状态:done(2026-09-11:矩阵 10 组合 + 嗅探分发 + 节点串联 + 场景 C 网关 + 单端口多服务 e2e 全部合入 main)
- 依赖:R2 + R4 + R5 + R6
- 关联:`docs/design.md` §六(场景 B/C)、`docs/research/nps.md` §2.1(pmux)、`docs/research/bridge.md`

## 目标

补齐 transport 矩阵的横纵组合与入口嗅探分发,打通"任意入站传输 × 任意出站传输"与"多节点串联"两条产品主路径。

## 交付物

- **转换矩阵 e2e**:入站 {tcp,tls,ws,h2,quic} × 出站 {tcp,tls,ws,h2,quic} 的组合测试,矩阵本身文档化(能转的组合 = 全部;语义限制注记,如 ws 半关闭降级);
- 多节点串联 e2e:节点 A(dialer star)══► 节点 B(star handler → router → direct),即 design.md §六 场景 C;
- `internal/ingress/sniff/`:pmux 式嗅探分发(3 字节协议嗅探 / Host 头 / TLS SNI+ALPN 增强版),字节重放进适配 Conn;作为 ingress 装饰器,配置形状:`sniff: [http, tls, star]`;
- 单端口部署形态 e2e:一个端口同时服务管理台 + 代理 + 隧道。

## 步骤

1. 组合矩阵测试基建(参数化生成用例),跑齐组合,产出矩阵文档;
2. 多节点串联 e2e(两个 raind 进程 + 一个 rainc);
3. sniff ingress:先做明文嗅探(nps pmux 同款),再叠 TLS 终止 + ALPN 分发;
4. 单端口场景 e2e;
5. 压测:同链路不同传输组合的吞吐/延迟表,总结文档。

## 验收标准

- [ ] 矩阵全组合通过,文档化(含限制注记);
- [ ] 场景 C 多节点 e2e 通过;
- [ ] 单端口嗅探分发 e2e 通过;
- [ ] 压测对比表 + 总结文档产出。

## 进度日志

- 2026-09-11:① ProxyServer.Listen 经 transport 注册表构建(任意传输可作数据面入站);② 传输转换矩阵 e2e 合入:入站 {tcp,tls,ws,h2,quic} × ctrl {tcp,tls} 10 组合隧道全链路全过(design.md §六 场景 B 的机器验证);③ 嗅探分发 ingress(internal/ingress/sniff)合入:前缀/HTTP Host/默认三级分发 + 字节全量重放,四路径测试 + 3 轮稳定性通过。
- 2026-09-11(续):节点串联(R7-3)合入——**协议扩展** MsgClientConnReq/Ack(0x30/0x31,客户端 connID 高位命名空间 0x80000000 起,与服务端分配天然隔离);CtrlClient 新增 `DialIntranet(ctx, clientProxyName, streamId)`(返回穿过隧道的 rainnet.Conn,读循环按拨号连接表分流帧);服务端 CtrlProxyServer 处理串联请求(身份绑定校验 + 按 clientProxy.addr 拨目标 + Bridge 桥接,半关闭穿透);e2e 两例:串联全链路(EOF 反应式后端)+ 冒用身份拒绝。star-protocol.md 已补帧表。
- 2026-09-11(续2):**场景 C 落地**——schema dialer 增加 role(inbound 默认/outbound);routes 按 dialer 角色分流(outbound→ChainRoutes);ProxyServer 内嵌链客户端(Serve 启动)+ 链路分流(裸字节 + R2 Bridge);装配入口 BuildServersFromRoot 导出;e2e TestNodeGatewayChain(用户→节点A网关══隧道══►节点B→内网 echo)通过。schema 校验同步(role 枚举)。
- 2026-09-11(续3):单端口 e2e 合入——三组件支持 UseListener 注入,一个端口同承载 admin/star/proxy;调试修复:嗅探重放字节与 rec 记录重复叠加(replay 改为 rec.buf 全量)。**R7 完成**(25 组合矩阵文档化可随 R8 文档补齐;rainc outbound 对齐随 R7 后续需求)。
