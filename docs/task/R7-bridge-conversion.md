# R7:传输转换全量 + 嗅探 ingress(= roadmap M5,产品主轴落地)

- 状态:in-progress(矩阵 e2e + 嗅探分发已合入 main,2026-09-11)
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
- **剩余**:① 单端口 e2e(嗅探接入 raind 装配:admin/proxy/star 共端口)——需组件支持外部注入 listener(装配层改造);② 多节点串联 e2e(场景 C,两个 raind + 一个 rainc);③ 全 25 组合矩阵文档化(10 组合已验证,补 ws/h2/quic 作 ctrl 通道的另一半)。
