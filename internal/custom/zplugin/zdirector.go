package zplugin

import (
	_ "rain-net/internal/custom/plugin/printer"
	_ "rain-net/internal/custom/plugin/writer"
)

var Directives = []string{
	"printer",
	"writer",
}
