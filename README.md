# rain-net
网络

2025.12.22

本来想着用pluginer来代替caddy，实现插件机制。现在发现，插件机制是coreDNS开发的，使用的是caddy的服务启动监听机制，因此pluginer命名不对。

构造链式插件
2025.12.23

额,名字没问题。caddy会先加载所有插件存储在	plugins = make(map[string]map[string]Plugin) 中,然后使用的时候,根据配置文件,把插件加载到Context.Plugin中,在serveDNS的时候构造链式插件