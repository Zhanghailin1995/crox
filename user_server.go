package crox

import (
	"fmt"
	"github.com/panjf2000/gnet/v2"
	"log"
	"sync"
	"sync/atomic"
)

type UserChannelContext struct {
	conn           gnet.Conn            // user channel
	userId         uint64               // 用户id
	lan            string               // 需要代理的本地内网地址
	nextChannelCtx *ProxyChannelContext // 代理连接，通过这条连接代理服务器将收到的数据转发到代理客户端，再由代理客户端转发给真正的服务器
	mu             sync.RWMutex
}

func (ctx *UserChannelContext) SetNextChannelCtx(nextChannelCtx *ProxyChannelContext) {
	ctx.mu.Lock()
	ctx.nextChannelCtx = nextChannelCtx
	ctx.mu.Unlock()
}

func (ctx *UserChannelContext) GetNextChannelCtx() *ProxyChannelContext {
	ctx.mu.RLock()
	nextChannelCtx := ctx.nextChannelCtx
	ctx.mu.RUnlock()
	return nextChannelCtx
}

func (ctx *UserChannelContext) SetConn(conn gnet.Conn) {
	ctx.mu.Lock()
	ctx.conn = conn
	ctx.mu.Unlock()
}

func (ctx *UserChannelContext) GetConn() gnet.Conn {
	ctx.mu.RLock()
	conn := ctx.conn
	ctx.mu.RUnlock()
	return conn
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
	log.Printf("user server on open")
	ctx := &UserChannelContext{
		conn: c,
	}
	c.SetContext(ctx)
	cmdChannelCtx0, ok := s.proxyServer.portCmdChannelCtxMap.Load(s.port)
	if !ok {
		log.Printf("port :%d bin channel not found", s.port)
		action = gnet.Close
		return
	} else {

		userId := NewUserId()
		lan := s.proxyServer.cfg.GetLan(s.port)

		ctx.mu.Lock()
		ctx.userId = userId
		ctx.lan = lan
		ctx.mu.Unlock()

		cmdChannelCtx := cmdChannelCtx0.(*ProxyChannelContext)
		cmdConn := cmdChannelCtx.GetConn()
		cmdChannelCtx.userChannelCtxMap.Store(userId, ctx)
		// TODO send proxy msg to proxy client, tell client there is a new connect into
		pkt := NewConnectPacket(userId, lan)
		buf := Encode(pkt)
		_ = cmdConn.AsyncWrite(buf, nil)
	}
	return
}

func (s *UserServer) OnTraffic(c gnet.Conn) (action gnet.Action) {
	log.Printf("user server on traffic")
	ctx := c.Context().(*UserChannelContext)

	nextChannelCtx := ctx.GetNextChannelCtx()

	nextConn := nextChannelCtx.GetConn()

	log.Println("read msg from user client")

	if nextConn == nil {
		action = gnet.Close
		log.Println("next channel is nil")
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
			pkt := NewDataPacket(buf)
			msg := Encode(pkt)
			err = nextConn.AsyncWrite(msg, nil)
			if err != nil {
				log.Printf("write to proxy channel error: %v\n", err)
				return gnet.Close
			}
			log.Println("write data packet to proxy channel success")
		}
	}
	return
}

func (s *UserServer) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	ctx := c.Context().(*UserChannelContext)
	userId := ctx.userId
	// lan := ctx.lan
	cmdChannelCtx0, ok := s.proxyServer.portCmdChannelCtxMap.Load(s.port)
	if !ok {
		log.Printf("port :%d bin channel not found", s.port)
		action = gnet.Close
		return
	} else {
		cmdChannelCtx := cmdChannelCtx0.(*ProxyChannelContext)

		cmdChannelCtx.userChannelCtxMap.Delete(userId)
		proxyChannelCtx := ctx.GetNextChannelCtx()
		if proxyChannelCtx != nil {
			proxyConn := proxyChannelCtx.GetConn()
			// do not close the channel
			proxyChannelCtx.mu.Lock()
			// reset some attributes
			proxyChannelCtx.nextChannelCtx = nil
			proxyChannelCtx.clientId = ""
			proxyChannelCtx.channelType = -1
			proxyChannelCtx.userId = 0
			proxyChannelCtx.mu.Unlock()

			pkt := NewDisconnectPacket(userId)
			buf := Encode(pkt)
			_ = proxyConn.AsyncWrite(buf, nil)
		}
	}
	return
}

var UserId uint64 = 1

func NewUserId() uint64 {
	return atomic.AddUint64(&UserId, 1)
}
