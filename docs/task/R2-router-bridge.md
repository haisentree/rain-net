# R2:Router + Bridge + 基础装饰器

- 状态:pending
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

## 验收标准

- [ ] 半关闭场景测试:客户端 CloseWrite 后服务端响应完整送达,不提前断;
- [ ] 任一方向读错误 → 有限时间内双端释放(ctx 取消生效);
- [ ] admin API 流量统计数字与装饰器记账一致;
- [ ] `go test ./...` 全绿。

## 进度日志

- (无)
