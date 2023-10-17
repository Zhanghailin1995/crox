package crox

import (
	"encoding/json"
	"testing"
)

func TestLoadProxyConfig(t *testing.T) {
	proxyConfig := LoadProxyConfig("config.json")
	if proxyConfig == nil {
		t.Error("load proxy config failed")
	}
	// check proxy config
	marshal, err := json.Marshal(proxyConfig)
	if err != nil {
		t.Error("marshal proxy config failed")
	}
	println(string(marshal))
}
