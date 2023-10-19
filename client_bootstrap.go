package crox

import (
	"container/list"
	"context"
	"github.com/panjf2000/gnet/v2"
	"log"
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
		clientId:             clientId,
		proxyAddr:            proxyAddr,
		proxyChannelCtxQueue: new(list.List),
	}

	proxyGnetCli, err := gnet.NewClient(proxyClient, gnet.WithMulticore(true), gnet.WithReusePort(true))
	if err != nil {
		log.Fatalf("create proxy client error %v\n", err)
	}
	proxyClient.proxyCli = proxyGnetCli
	cmdConn, err := proxyGnetCli.Dial("tcp", proxyAddr)
	if err != nil {
		log.Fatalf("connect proxy server error %v\n", err)
	}
	log.Printf("connect proxy server %s success\n", proxyAddr)
	cmdCtx := &ClientProxyChannelContext{
		conn:         cmdConn,
		lastReadTime: 0,
	}
	proxyClient.cmdChannelCtx = cmdCtx

	realServerClient := &RealServerClient{}
	realServerGnetCli, err := gnet.NewClient(realServerClient, gnet.WithMulticore(true), gnet.WithReusePort(true))
	if err != nil {
		log.Fatalf("create real server client error %v\n", err)
	}
	realServerClient.gnetCli = realServerGnetCli

	proxyClient.realServerCli = realServerClient
	boot.proxyClient = proxyClient
	boot.realServerClient = realServerClient

	proxyGnetCli.Start()
	realServerGnetCli.Start()

	authPkt := NewAuthPacket(clientId)
	buf := Encode(authPkt)
	cmdConn.AsyncWrite(buf, func(c gnet.Conn, err error) error {
		if err != nil {
			log.Printf("write auth packet error %v\n", err)
			cancel()
			return nil
		}
		c.SetContext(cmdCtx)
		return nil
	})

	<-shutdownCtx.Done()
	log.Println("client bootstrap shutdown")
}
