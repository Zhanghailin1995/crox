package crox

import (
	"context"
	"crox/pkg/logging"
	"fmt"
	"sync"
)

type ProxyServerBootstrap struct {
	proxyServer *ProxyServer
	cfgFilePath string
	cfg         *ProxyConfig
	userServers sync.Map
	shutdownCtx context.Context
	mu          sync.Mutex
}

func (boot *ProxyServerBootstrap) Boot() {
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
			port:        port,
			proxyServer: proxyServer,
		}
		userServer.Start()
		boot.userServers.Store(port, userServer)
	}
	SetP(boot)
	// 4. wait shutdown
	<-boot.shutdownCtx.Done()
	logging.Infof("server shutdown")
}

func (boot *ProxyServerBootstrap) UpdateConfig() {
	newConfigs, err := loadConfig(boot.cfgFilePath)
	if err != nil {
		logging.Errorf("load config error when update config %v", err)
		return
	}
	// 对比两个配置，找出新增的端口，找出删除的端口
	newLanInfo := make(map[uint32]string)
	newClientInetPortMapping := make(map[string][]uint32)
	for _, cfg := range newConfigs {
		clientId := cfg.ClientKey
		for _, mapping := range cfg.ProxyMappings {
			newLanInfo[uint32(mapping.InetPort)] = mapping.Lan
			ports, ok := newClientInetPortMapping[cfg.ClientKey]
			if !ok {
				ports = []uint32{}
			}
			newClientInetPortMapping[clientId] = append(ports, uint32(mapping.InetPort))
		}
	}

	var add []uint32
	var del []uint32
	var update []uint32

	// 1. 找出新增的端口
	boot.cfg.mu.RLock()
	for port, lan := range newLanInfo {
		oriLan, ok := boot.cfg.lanInfo[port]
		if !ok {
			add = append(add, port)
		} else if oriLan != lan {
			update = append(update, port)
		}
	}

	// 2. 找出删除的端口
	for port, _ := range boot.cfg.lanInfo {
		_, ok := newLanInfo[port]
		if !ok {
			del = append(del, port)
		}
	}
	boot.cfg.mu.RUnlock()

	// 3. 更新配置
	boot.cfg.mu.Lock()
	defer boot.cfg.mu.Unlock()

	boot.mu.Lock()
	boot.cfg.lanInfo = newLanInfo
	boot.cfg.clientInetPortMapping = newClientInetPortMapping
	boot.cfg.rawConfigs = newConfigs
	boot.mu.Unlock()

	for _, port := range add {
		userServer := &UserServer{
			network:     "tcp",
			addr:        fmt.Sprintf(":%d", port),
			port:        port,
			proxyServer: boot.proxyServer,
		}
		userServer.Start()
		boot.userServers.Store(port, userServer)
	}

	for _, port := range del {
		userServer, ok := boot.userServers.Load(port)
		if ok {
			userServer.(*UserServer).Shutdown()
			boot.userServers.Delete(port)
		}
	}

}
