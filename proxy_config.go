package crox

import (
	"crox/pkg/logging"
	"encoding/json"
	"io/ioutil"
	"sync"
)

type Config struct {
	Name          string         `json:"name"`
	ClientKey     string         `json:"clientKey"`
	ProxyMappings []ProxyMapping `json:"proxyMappings"`
}

type ProxyMapping struct {
	InetPort int    `json:"inetPort"`
	Lan      string `json:"lan"`
	Name     string `json:"name"`
}

func LoadProxyConfig(path string) *ProxyConfig {
	proxyConfig := &ProxyConfig{
		lanInfo:               make(map[uint32]string),
		clientInetPortMapping: make(map[string][]uint32),
	}
	configs, err := loadConfig(path)
	if err != nil {
		logging.Fatalf("load config failed: %v", err)
		return nil
	}
	for _, cfg := range configs {
		clientId := cfg.ClientKey
		for _, mapping := range cfg.ProxyMappings {
			proxyConfig.lanInfo[uint32(mapping.InetPort)] = mapping.Lan
			ports, ok := proxyConfig.clientInetPortMapping[cfg.ClientKey]
			if !ok {
				ports = []uint32{}
			}
			proxyConfig.clientInetPortMapping[clientId] = append(ports, uint32(mapping.InetPort))
		}
	}
	proxyConfig.rawConfigs = configs
	return proxyConfig
}

func loadConfig(path string) ([]Config, error) {
	// load config from local file
	// 1. read file
	// 2. parse json
	// 3. return config
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var configs []Config
	err = json.Unmarshal(data, &configs)
	if err != nil {
		return nil, err
	}
	return configs, nil
}

type ProxyConfig struct {
	lanInfo               map[uint32]string
	clientInetPortMapping map[string][]uint32
	rawConfigs            []Config
	mu                    sync.RWMutex
}

func (cfg *ProxyConfig) GetRawConfigs() []Config {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	return cfg.rawConfigs
}

func (cfg *ProxyConfig) GetClientInetPorts(clientKey string) []uint32 {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	return cfg.clientInetPortMapping[clientKey]
}

func (cfg *ProxyConfig) GetLan(port uint32) string {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()
	v, ok := cfg.lanInfo[port]
	if ok {
		return v
	} else {
		return ""
	}
}
