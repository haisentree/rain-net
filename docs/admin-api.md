# 管理 API（admin 监听器）

`type: admin` 监听器提供运行时查询与配置能力。鉴权：请求头 `Authorization: Bearer <adminPassword>`；**未配置 `adminPassword` 时接口拒绝一切请求**（安全默认）。

**内置 Web 管理台**：浏览器直接访问 admin 监听器地址（如 `http://127.0.0.1:5173/`），输入 adminPassword 即可使用——总览大盘、在线客户端（踢下线）、流与流量统计、活跃连接、路由规则增删、凭据发放/吊销。静态资源内嵌于二进制（`internal/star/starserver/web/`），无需单独部署；`/api/*` 走 token 鉴权，页面本身公开（无敏感数据）。

```yaml
- name: listener-5
  type: admin
  transport: tcp
  addr: 127.0.0.1:5173        # 建议只绑内网/本机,或启用 settings.tls
  settings:
    adminPassword: admin-secret
```

> 注意：动态修改只作用于运行时，进程重启后以配置文件为准（持久化后续接入配置中心）。

## 查询接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/status | 总览：客户端数/流数/连接数/累计流量/运行时长 |
| GET | /api/tunnel/clients | 在线客户端（名称、身份、来源、接入时间、流数） |
| GET | /api/streams | 已注册流，含 `inBytes`（用户→内网）/`outBytes`（内网→用户）累计流量 |
| GET | /api/tunnel/conns | 当前活跃连接 |
| GET | /api/tunnel/connects | 当前全部路由规则（按 proxyName 分组） |
| GET | /api/tunnel/credentials | 配置中的客户端凭据/流定义（Web 管理台凭据页数据源） |

## 管理接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/tunnel/credentials | 动态发放客户端凭据/流定义（body 为 ClientProxy JSON） |
| DELETE | /api/tunnel/credentials/{clientProxyName}/{streamId}?kick=1 | 删除凭据；`kick=1` 时同时踢下线 |
| POST | /api/tunnel/connects | 动态添加路由规则（body 为 ConnectItem JSON） |
| DELETE | /api/tunnel/connects/{proxyName}/{clientProxyName}/{streamId} | 删除路由规则 |
| POST | /api/tunnel/kick | 踢指定身份的客户端全部会话下线（body: `{"clientProxyName": "..."}`） |

动态添加的 clientProxy 即刻成为可用凭据，客户端可直接拨号注册；connect 规则增删即时影响路由（下一条外部连接生效），删掉某流的全部规则后流量自动向其余规则故障转移。

## 组件自注册(APIProvider)

实现了 `pluginer.APIProvider`(`RegisterAPI(mux *http.ServeMux)`)的组件会在 admin 启动时
向 apiMux 注册自己的管理子路由;鉴权统一走 Bearer token,组件不做自己的鉴权(design.md §七)。

## 示例

```bash
TOKEN="Authorization: Bearer admin-secret"

# 总览
curl -s -H "$TOKEN" http://127.0.0.1:5173/api/status

# 发放一个新客户端凭据并接入路由
curl -s -H "$TOKEN" -H "Content-Type: application/json" \
  -d '{"clientProxyName":"clientProxy-9","streamId":"dailer-9-stream-1","addr":"127.0.0.1:9999","transport":"tcp","keyPassword":"pwd-9"}' \
  http://127.0.0.1:5173/api/tunnel/credentials
curl -s -H "$TOKEN" -H "Content-Type: application/json" \
  -d '{"proxyName":"listener-proxy-0","clientProxyName":"clientProxy-9","streamId":"dailer-9-stream-1"}' \
  http://127.0.0.1:5173/api/tunnel/connects

# 查看流量 / 踢下线 / 吊销凭据
curl -s -H "$TOKEN" http://127.0.0.1:5173/api/streams
curl -s -H "$TOKEN" -H "Content-Type: application/json" \
  -d '{"clientProxyName":"clientProxy-9"}' http://127.0.0.1:5173/api/tunnel/kick
curl -s -H "$TOKEN" -X DELETE "http://127.0.0.1:5173/api/tunnel/credentials/clientProxy-9/dailer-9-stream-1"
```
