package starserver

import (
	"embed"
)

// web 管理台静态资源(构建期嵌入,单二进制自带 UI)
//
//go:embed web
var webFS embed.FS
