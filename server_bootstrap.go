package crox

import (
	"context"
	"crox/pkg/logging"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"sync"
)

type SvrBootstrap struct {
	proxy *ProxyServerBootstrap
	web   *WebServerBootstrap
}

func (boot *SvrBootstrap) Boot() {
	go boot.proxy.Boot()
	boot.web.Boot()
}

type ProxyServerBootstrap struct {
	proxyServer *ProxyServer
	cfgFilePath string
	cfg         *ProxyConfig
	userServers sync.Map
	shutdownCtx context.Context
	mu          sync.Mutex
}

var ErrPortAlreadyBind = errors.New("port already bind")

var ErrProxyPortNotOpen = errors.New("proxy port not open")

var ErrUserProxySvrNotFound = errors.New("user proxy server not found")

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
	boot.cfg.lanInfo = newLanInfo
	boot.cfg.clientInetPortMapping = newClientInetPortMapping
	boot.cfg.rawConfigs = newConfigs
	boot.cfg.mu.Unlock()

	boot.mu.Lock()
	defer boot.mu.Unlock()

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

func (boot *ProxyServerBootstrap) AddClient(clientId string, clientName string) error {
	boot.cfg.mu.Lock()
	defer boot.cfg.mu.Unlock()
	boot.cfg.clientInetPortMapping[clientId] = []uint32{}
	boot.cfg.rawConfigs = append(boot.cfg.rawConfigs, Config{
		ClientKey:     clientId,
		Name:          clientName,
		ProxyMappings: []ProxyMapping{},
	})
	content, err := json.Marshal(boot.cfg.rawConfigs)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(boot.cfgFilePath, content, 0644)
}

func (boot *ProxyServerBootstrap) AddProxy(clientId string, proxy *ProxyMapping) error {
	boot.cfg.mu.Lock()
	_, ok := boot.cfg.lanInfo[uint32(proxy.InetPort)]
	if ok {
		boot.cfg.mu.Unlock()
		return ErrPortAlreadyBind
	}
	boot.cfg.lanInfo[uint32(proxy.InetPort)] = proxy.Lan
	boot.cfg.clientInetPortMapping[clientId] = append(boot.cfg.clientInetPortMapping[clientId], uint32(proxy.InetPort))
	var cfgs []Config
	for _, cfg := range boot.cfg.rawConfigs {
		if cfg.ClientKey == clientId {
			cfg.ProxyMappings = append(cfg.ProxyMappings, *proxy)
		}
		cfgs = append(cfgs, cfg)
	}
	boot.cfg.rawConfigs = cfgs
	content, err := json.Marshal(boot.cfg.rawConfigs)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(boot.cfgFilePath, content, 0644)
	if err != nil {
		return err
	}
	boot.cfg.mu.Unlock()

	boot.mu.Lock()
	defer boot.mu.Unlock()

	userServer := &UserServer{
		network:     "tcp",
		addr:        fmt.Sprintf(":%d", proxy.InetPort),
		port:        uint32(proxy.InetPort),
		proxyServer: boot.proxyServer,
	}
	userServer.Start()
	boot.userServers.Store(uint32(proxy.InetPort), userServer)
	return nil
}

func (boot *ProxyServerBootstrap) DeleteProxy(clientId string, port int) error {
	boot.cfg.mu.Lock()
	_, ok := boot.cfg.lanInfo[uint32(port)]
	if ok {
		boot.cfg.mu.Unlock()
		return ErrProxyPortNotOpen
	}
	delete(boot.cfg.lanInfo, uint32(port))
	ports := boot.cfg.clientInetPortMapping[clientId]
	// remove port from ports
	var newPorts []uint32
	for _, p := range ports {
		if p != uint32(port) {
			newPorts = append(newPorts, p)
		}
	}
	boot.cfg.clientInetPortMapping[clientId] = newPorts
	var cfgs []Config

	for _, cfg := range boot.cfg.rawConfigs {
		if cfg.ClientKey == clientId {
			var newMappings []ProxyMapping
			for _, mapping := range cfg.ProxyMappings {
				if mapping.InetPort != port {
					newMappings = append(newMappings, mapping)
				}
			}
			cfg.ProxyMappings = newMappings
		}
		cfgs = append(cfgs, cfg)
	}
	boot.cfg.rawConfigs = cfgs
	content, err := json.Marshal(boot.cfg.rawConfigs)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(boot.cfgFilePath, content, 0644)
	if err != nil {
		return err
	}
	boot.cfg.mu.Unlock()

	boot.mu.Lock()
	defer boot.mu.Unlock()
	userSvr0, ok := boot.userServers.Load(uint32(port))
	if !ok {
		return ErrUserProxySvrNotFound
	}
	userSvr := userSvr0.(*UserServer)
	userSvr.Shutdown()
	boot.userServers.Delete(uint32(port))
	return nil
}
