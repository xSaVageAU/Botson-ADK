package main

import (
	"botson/gateways"
)

// gatewayMgr holds the currently active gateway manager.
// It is started, stopped, and swapped by main() and the OnReload hook.
var gatewayMgr *gateways.GatewayManager
