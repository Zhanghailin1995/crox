package crox

import (
	"context"
	"crox/pkg/logging"
	"crox/pkg/util"
	"fmt"
	"github.com/panjf2000/gnet/v2"
	"sync"
	"sync/atomic"
)

type UserConnContext struct {
	ctxId       string
	conn        gnet.Conn         // user conn
	userId      uint64            // 用户id
	lan         string            // 需要代理的本地内网地址
	nextConnCtx *ProxyConnContext // 代理连接，通过这条连接代理服务器将收到的数据转发到代理客户端，再由代理客户端转发给真正的服务器
	mu          sync.RWMutex
}

func (ctx *UserConnContext) GetUserId() uint64 {
	ctx.mu.RLock()
	userId := ctx.userId
	ctx.mu.RUnlock()
	return userId
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
	network, addr := s.network, s.addr
	go func() {
		err := gnet.Run(s, fmt.Sprintf("%s://%s", network, addr), gnet.WithMulticore(true), gnet.WithReusePort(true))
		if err != nil {
			logging.Errorf("start server: %s://%s error: %v", network, addr, err)
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
	logging.Infof("user server: %s://%s accept a new conn: %s", s.network, s.addr, c.RemoteAddr())
	ctx := &UserConnContext{
		ctxId: util.ContextId(),
		conn:  c,
	}
	c.SetContext(ctx)
	cmdConnCtx0, ok := s.proxyServer.portCmdConnCtxMap.Load(s.port)
	if !ok {
		logging.Warnf("can not found cmd conn, port: %d", s.port)
		action = gnet.Close
		return
	} else {
		userId := NewUserId()
		lan := s.proxyServer.cfg.GetLan(s.port)
		if lan == "" {
			logging.Errorf("can not found lan info, port: %d", s.port)
			action = gnet.Close
			return
		}

		ctx.mu.Lock()
		ctx.userId = userId
		ctx.lan = lan
		ctx.mu.Unlock()

		cmdConnCtx := cmdConnCtx0.(*ProxyConnContext)
		cmdConn := cmdConnCtx.GetConn()
		logging.Infof("user server on open, userId: %d, lan: %s", userId, lan)
		cmdConnCtx.userConnCtxMap.Store(userId, ctx)
		pkt := newConnectPacket(userId, lan)
		buf := Encode(pkt)
		_ = cmdConn.AsyncWrite(buf, nil)
	}
	return
}

func (s *UserServer) OnTraffic(c gnet.Conn) (action gnet.Action) {
	ctx := c.Context().(*UserConnContext)
	userId := ctx.GetUserId()
	nextConnCtx := ctx.GetNextConnCtx()
	if nextConnCtx == nil {
		logging.Warnf("read from user conn, userId: %d, but next conn is nil", userId)
		return
	}
	nextConn := nextConnCtx.GetConn()

	if nextConn == nil {
		action = gnet.Close
		logging.Warnf("read from user conn, userId: %d, but next conn is nil", userId)
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
				logging.Errorf("read from user conn error, userId: %d, error: %v", userId, err)
				return gnet.Close
			}
			pkt := newDataPacket(buf)
			msg := Encode(pkt)
			err = nextConn.AsyncWrite(msg, nil)
			if err != nil {
				logging.Errorf("user id: %d, write to next conn error: %v", userId, err)
				return gnet.Close
			}
			logging.Debugf("user id: %d, write data packet to next conn success", userId)
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

			pkt := newDisconnectPacket(userId)
			buf := Encode(pkt)
			_ = proxyConn.AsyncWrite(buf, nil)
		}
	}
	return
}

func (s *UserServer) Shutdown() {
	_ = s.eng.Stop(context.Background())
}

var UserId uint64 = 1

func NewUserId() uint64 {
	return atomic.AddUint64(&UserId, 1)
}
