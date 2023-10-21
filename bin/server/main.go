package main

import (
	"crox"
	"crox/pkg/logging"
)

func main() {
	logging.DefaultLogger()
	bootServer := &crox.ProxyServerBootstrap{}
	bootServer.Boot()
}
