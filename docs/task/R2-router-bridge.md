# R2:Router + Bridge + 基础装饰器

- 状态:done(2026-09-11)
- 依赖:R1
- 关联:`docs/design.md` §二(Bridge 半关闭矩阵)、§三(Router 顺序匹配)、§九.3/4/5/6;`docs/research/bridge.md`(取舍依据)

## 目标

落地中间段:路由表升级为 Router(顺序首中匹配),Bridge 按半关闭传播契约实现,基础 Conn 装饰器(记账/限速/admission)就位。

## 交付物

- `internal/router/`:connecttable 迁移升级;匹配规则 = 配置声明顺序、首中即停(§九.5);保留 round-robin + 故障转移;
- `internal/bridge/`:`Bridge(src, dst rainnet.Conn) error` 纯函数形状(抄 gost Transport);半关闭传播矩阵完整实现(含排空 deadline,§九遗留问题);池化 buffer(32–64KB,benchmark 后定);
- 基础装饰器(§九.6):记账(in/outBytes)、限速、admission(单 ingress 连接数上限、单 identity 速率上限),均为 `rainnet.Conn` 包装器;
- 错误三分类(网络错/协议错/策略拒绝,§九.4);
- Bridge 遗留问题收尾:排空 deadline、能力不对等矩阵补全、记账接口与 streamtable 对齐(见 `docs/research/bridge.md` §七)。

## 步骤

1. 迁移 connecttable → router,定死顺序匹配语义并加单测;
2. 实现 bridge + 半关闭矩阵,对"请求后关写等响应"协议行为写专项测试;
3. 实现三个装饰器,接入现有流量统计(admin API 的 inBytes/outBytes 数据源切换);
4. conformance 增加 bridge 场景(跨 Conn 半关闭传播);
5. e2e 全绿。

## 验收结果

- [x] 半关闭场景:客户端 CloseWrite 后,HTTP/1.0 风格后端的完整响应穿过 Bridge 送达,之后才 EOF(TestBridgeHalfClose);
- [x] 读错误 → 立即双关(0.00s);ctx 取消 → 返回 context.Canceled 且 0.05s 内双端释放;
- [x] 排空超时:对端装死 200ms 内触发双关(WithDrainTimeout);
- [x] 记账装饰器单测通过(In/Out 精确计数);**与 admin API 的数据源切换在 R3**(starserver 流统计仍走 streamtable,桥接路径接入后统一);
- [x] `go test ./...` 全绿(9 个测试包)。

## 进度日志

- 2026-09-11:完成。internal/router(顺序首中 + 按规则 round-robin,connecttable 在 R3 拆 star 时切换并删除);internal/bridge(双 pump + 半关闭传播 + DrainTimeout + ctx 取消,池化 64KB buffer);internal/decorator(记账/限速/admission + 最小令牌桶);pkg/rainnet 错误三分类(Network/Protocol/Policy)。
- 调试记录:① conformance 思路复用——Bridge 测试初版把 client(拨号端)误接为 Bridge 的 src,数据过网线到了无人读的 accept 端,靠 goroutine dump 定位;② 令牌桶初版 take(n>burst) 永远无法满足导致限速测试死锁,改为分次取用;③ 对端干净 Close 只产生 EOF,要测错误传播需 SO_LINGER=0 制造 RST。分支 r2-router-bridge。
