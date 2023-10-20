package crox

import (
	"crox/pkg/logging"
	"fmt"
	"github.com/panjf2000/gnet/v2"
	"log"
	"sync"
	"sync/atomic"
)

type UserConnContext struct {
	conn        gnet.Conn         // user conn
	userId      uint64            // 用户id
	lan         string            // 需要代理的本地内网地址
	nextConnCtx *ProxyConnContext // 代理连接，通过这条连接代理服务器将收到的数据转发到代理客户端，再由代理客户端转发给真正的服务器
	mu          sync.RWMutex
}

func (ctx *UserConnContext) SetNextConnCtx(nextConnCtx *ProxyConnContext) {
	ctx.mu.Lock()
	ctx.nextConnCtx = nextConnCtx
	ctx.mu.Unlock()
}

func (ctx *UserConnContext) GetNextConnCtx() *ProxyConnContext {
	ctx.mu.RLock()
	nextConnCtx := ctx.nextConnCtx
	ctx.mu.RUnlock()
	return nextConnCtx
}

func (ctx *UserConnContext) SetConn(conn gnet.Conn) {
	ctx.mu.Lock()
	ctx.conn = conn
	ctx.mu.Unlock()
}

func (ctx *UserConnContext) GetConn() gnet.Conn {
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
			log.Fatalf("start server error %v", err)
		}
	}()
}

func (s *UserServer) OnBoot(eng gnet.Engine) (action gnet.Action) {
	lan := s.proxyServer.cfg.GetLan(s.port)
	logging.Infof("running user server:%s, proxy: %s", fmt.Sprintf("%s://%s", s.network, s.addr), lan)
	s.eng = eng
	return
}

func (s *UserServer) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	logging.Infof("user server on open")
	ctx := &UserConnContext{
		conn: c,
	}
	c.SetContext(ctx)
	cmdConnCtx0, ok := s.proxyServer.portCmdConnCtxMap.Load(s.port)
	if !ok {
		logging.Infof("port :%d cmd conn not found", s.port)
		action = gnet.Close
		return
	} else {

		userId := NewUserId()
		lan := s.proxyServer.cfg.GetLan(s.port)

		ctx.mu.Lock()
		ctx.userId = userId
		ctx.lan = lan
		ctx.mu.Unlock()

		cmdConnCtx := cmdConnCtx0.(*ProxyConnContext)
		cmdConn := cmdConnCtx.GetConn()
		logging.Infof("user server on open, userId: %d, lan: %s", userId, lan)
		cmdConnCtx.userConnCtxMap.Store(userId, ctx)
		// TODO send proxy msg to proxy client, tell client there is a new connect into
		pkt := NewConnectPacket(userId, lan)
		buf := Encode(pkt)
		_ = cmdConn.AsyncWrite(buf, nil)
	}
	return
}

func (s *UserServer) OnTraffic(c gnet.Conn) (action gnet.Action) {
	logging.Infof("user server on traffic")
	ctx := c.Context().(*UserConnContext)

	nextConnCtx := ctx.GetNextConnCtx()

	nextConn := nextConnCtx.GetConn()

	logging.Infof("read msg from user client")

	if nextConn == nil {
		action = gnet.Close
		logging.Warnf("next conn is nil")
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
				logging.Infof("read from user conn error: %v", err)
				return gnet.Close
			}
			pkt := NewDataPacket(buf)
			msg := Encode(pkt)
			err = nextConn.AsyncWrite(msg, nil)
			if err != nil {
				logging.Infof("write to proxy conn error: %v", err)
				return gnet.Close
			}
			logging.Infof("write data packet to proxy conn success")
		}
	}
	return
}

func (s *UserServer) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	ctx := c.Context().(*UserConnContext)
	userId := ctx.userId
	// lan := ctx.lan
	cmdConnCtx0, ok := s.proxyServer.portCmdConnCtxMap.Load(s.port)
	if !ok {
		logging.Infof("port :%d bin conn not found", s.port)
		action = gnet.Close
		return
	} else {
		cmdConnCtx := cmdConnCtx0.(*ProxyConnContext)

		cmdConnCtx.userConnCtxMap.Delete(userId)
		proxyConnCtx := ctx.GetNextConnCtx()
		if proxyConnCtx != nil {
			proxyConn := proxyConnCtx.GetConn()
			// do not close conn
			proxyConnCtx.mu.Lock()
			// reset some attributes
			proxyConnCtx.nextConnCtx = nil
			proxyConnCtx.clientId = ""
			proxyConnCtx.connType = -1
			proxyConnCtx.userId = 0
			proxyConnCtx.mu.Unlock()

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
