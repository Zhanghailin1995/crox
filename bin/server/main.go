package main

import (
	"crox"
	"crox/pkg/logging"
)

func main() {
	logging.DefaultLogger()

	bootServer := &crox.SvrBootstrap{}
	bootServer.Boot()
}
