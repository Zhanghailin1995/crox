package crox

import (
	"container/list"
	"context"
	"crox/pkg/logging"
	"crox/pkg/util"
	"github.com/panjf2000/gnet/v2"
)

type ClientBootstrap struct {
	proxyClient      *ProxyClient
	realServerClient *RealServerClient
	shutdownCtx      context.Context
}

func (boot *ClientBootstrap) Boot(clientId string, proxyAddr string) {
	shutdownCtx, cancel := context.WithCancel(context.Background())
	boot.shutdownCtx = shutdownCtx
	proxyClient := &ProxyClient{
		clientId:          clientId,
		proxyAddr:         proxyAddr,
		proxyConnCtxQueue: new(list.List),
	}

	proxyGnetCli, err := gnet.NewClient(proxyClient, gnet.WithMulticore(true), gnet.WithReusePort(true))
	if err != nil {
		logging.Fatalf("create proxy client error %v", err)
		logging.Fatalf("create proxy client error %v", err)
	}
	proxyClient.proxyCli = proxyGnetCli
	cmdConn, err := proxyGnetCli.Dial("tcp", proxyAddr)
	if err != nil {
		logging.Fatalf("connect proxy server error %v", err)
	}
	logging.Infof("connect proxy server %s success", proxyAddr)
	cmdConnCtx := &ClientProxyConnContext{
		ctxId:        util.ContextId(),
		conn:         cmdConn,
		lastReadTime: 0,
		heartbeatSeq: 0,
	}
	proxyClient.cmdConnCtx = cmdConnCtx

	realServerClient := &RealServerClient{}
	realServerGnetCli, err := gnet.NewClient(realServerClient, gnet.WithMulticore(true), gnet.WithReusePort(true))
	if err != nil {
		logging.Fatalf("create real server client error %v", err)
	}
	realServerClient.gnetCli = realServerGnetCli

	proxyClient.realServerCli = realServerClient
	boot.proxyClient = proxyClient
	boot.realServerClient = realServerClient

	proxyGnetCli.Start()
	realServerGnetCli.Start()

	authPkt := newAuthPacket(clientId)
	buf := Encode(authPkt)
	cmdConn.AsyncWrite(buf, func(c gnet.Conn, err error) error {
		if err != nil {
			logging.Infof("write auth packet error %v", err)
			cancel()
			return nil
		}
		c.SetContext(cmdConnCtx)
		go startSendHeartbeat(cmdConnCtx)
		return nil
	})

	<-shutdownCtx.Done()
	logging.Infof("client bootstrap shutdown")
}
