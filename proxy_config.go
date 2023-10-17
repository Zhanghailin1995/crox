package crox

import (
	"encoding/json"
	"io/ioutil"
	"log"
)

type config struct {
	Name          string         `json:"name"`
	ClientKey     string         `json:"clientKey"`
	ProxyMappings []proxyMapping `json:"proxyMappings"`
}

type proxyMapping struct {
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
		log.Fatal("load config failed: ", err)
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

func loadConfig(path string) ([]config, error) {
	// load config from local file
	// 1. read file
	// 2. parse json
	// 3. return config
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var configs []config
	err = json.Unmarshal(data, &configs)
	if err != nil {
		return nil, err
	}
	return configs, nil
}

type ProxyConfig struct {
	lanInfo               map[uint32]string
	clientInetPortMapping map[string][]uint32
	rawConfigs            []config
}

func (cfg *ProxyConfig) GetClientInetPorts(clientKey string) []uint32 {
	return cfg.clientInetPortMapping[clientKey]
}

func (cfg *ProxyConfig) GetLan(port uint32) string {
	v, ok := cfg.lanInfo[port]
	if ok {
		return v
	} else {
		log.Fatal("invalid port: ", port)
		return ""
	}
}
