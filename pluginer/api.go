package pluginer

import "net/http"

// APIProvider 组件可选实现:向 admin 控制面注册自己的管理 API 子路由。
//
// 约定(docs/design.md §七 控制面):
//   - 实现方自行约定路径前缀(如 /api/tunnel/...),框架不理解内容,组件自治;
//   - 鉴权由 admin 监听器统一处理(Bearer token),实现方不做自己的鉴权;
//   - 插件卸载时其路由随组件一起消失,零残留。
type APIProvider interface {
	RegisterAPI(mux *http.ServeMux)
}
