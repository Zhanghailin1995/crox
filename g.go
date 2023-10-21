package crox

type G struct {
	p *ProxyServerBootstrap
}

var _g *G = &G{}

func SetP(p *ProxyServerBootstrap) {
	_g.p = p
}

func P() *ProxyServerBootstrap {
	return _g.p
}
