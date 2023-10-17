package crox

import (
	"context"
	"fmt"
	"log"
	"sync"
)

type ServerBootstrap struct {
	proxyServer *ProxyServer
	cfg         *ProxyConfig
	userServers sync.Map
	shutdownCtx context.Context
}

func (boot *ServerBootstrap) Boot() {
	shutdownCtx, cancel := context.WithCancel(context.Background())
	boot.shutdownCtx = shutdownCtx
	// 1. load config
	boot.cfg = LoadProxyConfig("config.json")
	// 2. start proxy server
	proxyServer := &ProxyServer{
		network: "tcp",
		addr:    ":7856",
		cfg:     boot.cfg,
	}

	boot.proxyServer = proxyServer
	proxyServer.Start(cancel)

	// 3. start user server
	for port, _ := range boot.cfg.lanInfo {
		userServer := &UserServer{
			network:     "tcp",
			addr:        fmt.Sprintf(":%d", port),
			proxyServer: proxyServer,
		}
		userServer.Start()
		boot.userServers.Store(port, userServer)
	}
	// 4. wait shutdown

	<-boot.shutdownCtx.Done()
	log.Println("server shutdown")
}
