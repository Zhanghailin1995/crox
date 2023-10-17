package crox

import (
	"fmt"
	"github.com/panjf2000/gnet/v2"
	"log"
	"sync/atomic"
)

type UserChannelContext struct {
	conn     gnet.Conn // user channel
	userId   uint64    // 用户id
	lan      string    // 需要代理的本地内网地址
	nextConn gnet.Conn // 代理连接，通过这条连接代理服务器将收到的数据转发到代理客户端，再由代理客户端转发给真正的服务器
}

// UserServer 一个代理端口开一个UserServer
type UserServer struct {
	gnet.BuiltinEventEngine
	eng         gnet.Engine
	network     string
	addr        string
	port        uint32
	proxyServer *ProxyServer
}

func (s *UserServer) Start() {
	go func() {
		err := gnet.Run(s, fmt.Sprintf("%s://%s", s.network, s.addr), gnet.WithMulticore(true), gnet.WithReusePort(true))
		if err != nil {
			log.Fatalf("start server error %v\n", err)
		}
	}()
}

func (s *UserServer) OnBoot(eng gnet.Engine) (action gnet.Action) {
	log.Printf("running user server on %s\n", fmt.Sprintf("%s://%s", s.network, s.addr))
	s.eng = eng
	return
}

func (s *UserServer) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	ctx := &UserChannelContext{
		conn: c,
	}
	c.SetContext(ctx)
	cmdChannel, ok := s.proxyServer.portCmdChannelMap.Load(s.port)
	if !ok {
		log.Printf("port :%d bin channel not found", s.port)
		action = gnet.Close
		return
	} else {
		userId := NewUserId()
		lan := s.proxyServer.cfg.GetLan(s.port)
		ctx.userId = userId
		ctx.lan = lan

		cmdConn := cmdChannel.(gnet.Conn)
		cmdCtx := cmdConn.Context().(*ProxyChannelContext)
		cmdCtx.userChannels.Store(userId, c)
		// TODO send proxy msg to proxy client, tell client there is a new connect into
		pkt := NewConnectPacket(userId, lan)
		buf := Encode(pkt)
		_ = cmdConn.AsyncWrite(buf, nil)
	}
	return
}

func (s *UserServer) onTraffic(c gnet.Conn) (action gnet.Action) {
	ctx := c.Context().(*UserChannelContext)
	nextConn := ctx.nextConn
	if nextConn == nil {
		action = gnet.Close
		return
	} else {
		for {
			n := c.InboundBuffered()
			if n <= 0 {
				// no more data
				break
			}
			buf := make([]byte, n)
			_, err := c.Read(buf)
			if err != nil {
				log.Printf("read from user channel error: %v\n", err)
				return gnet.Close
			}
			pkt := NewDataPaket(buf)
			msg := Encode(pkt)
			err = nextConn.AsyncWrite(msg, nil)
			if err != nil {
				log.Printf("write to proxy channel error: %v\n", err)
				return gnet.Close
			}
		}
	}
	return
}

func (s *UserServer) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	ctx := c.Context().(*UserChannelContext)
	userId := ctx.userId
	// lan := ctx.lan
	cmdChannel, ok := s.proxyServer.portCmdChannelMap.Load(s.port)
	if !ok {
		log.Printf("port :%d bin channel not found", s.port)
		action = gnet.Close
		return
	} else {
		cmdConn := cmdChannel.(gnet.Conn)
		cmdCtx := cmdConn.Context().(*ProxyChannelContext)
		cmdCtx.userChannels.Delete(userId)
		proxyChannel := ctx.nextConn
		if proxyChannel != nil {
			// do not close the channel
			proxyChannelContext := proxyChannel.Context().(*ProxyChannelContext)
			proxyChannelContext.nextConn = nil
			proxyChannelContext.clientId = ""
			proxyChannelContext.channelType = -1
			proxyChannelContext.userId = 0

			pkt := NewDisconnectPacket(userId)
			buf := Encode(pkt)
			_ = cmdConn.AsyncWrite(buf, nil)
		}
	}
	return
}

var UserId uint64 = 1

func NewUserId() uint64 {
	return atomic.AddUint64(&UserId, 1)
}
