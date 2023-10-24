package crox

import (
	"crox/pkg/logging"
	"github.com/panjf2000/gnet/v2"
	"sync"
)

type RealServerConnContext struct {
	ctxId       string
	userId      uint64
	conn        gnet.Conn
	nextConnCtx *ClientProxyConnContext
	mu          sync.RWMutex
}

func (ctx *RealServerConnContext) GetUserId() uint64 {
	ctx.mu.RLock()
	userId := ctx.userId
	ctx.mu.RUnlock()
	return userId
}

func (ctx *RealServerConnContext) GetNextConnCtx() *ClientProxyConnContext {
	ctx.mu.RLock()
	nextConnCtx := ctx.nextConnCtx
	ctx.mu.RUnlock()
	return nextConnCtx
}

func (ctx *RealServerConnContext) SetNextConnCtx(nextConnCtx *ClientProxyConnContext) {
	ctx.mu.Lock()
	ctx.nextConnCtx = nextConnCtx
	ctx.mu.Unlock()
}

func (ctx *RealServerConnContext) RemoveNextConnCtx(expect *ClientProxyConnContext) {
	ctx.mu.Lock()
	if ctx.nextConnCtx == expect {
		ctx.nextConnCtx = nil
	}
	ctx.mu.Unlock()
}

func (ctx *RealServerConnContext) GetConn() gnet.Conn {
	ctx.mu.RLock()
	conn := ctx.conn
	ctx.mu.RUnlock()
	return conn
}

type RealServerClient struct {
	*gnet.BuiltinEventEngine
	eng     gnet.Engine
	gnetCli *gnet.Client
}

func (client *RealServerClient) OnBoot(eng gnet.Engine) (action gnet.Action) {
	client.eng = eng
	return
}

func (client *RealServerClient) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	return
}

func (client *RealServerClient) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	context0 := c.Context()
	if context0 == nil {
		return gnet.None
	}
	context := context0.(*RealServerConnContext)
	if context != nil {
		nextConnCtx := context.GetNextConnCtx()
		if nextConnCtx != nil {

			context.RemoveNextConnCtx(nextConnCtx)
			nextConnCtx.RemoveNextConnCtx(context)

			ch := nextConnCtx.GetConn()
			pkt := newDisconnectPacket(context.GetUserId())
			buf := Encode(pkt)
			writeErr := ch.AsyncWrite(buf, nil)
			if writeErr != nil {
				logging.Infof("write to next conn error", writeErr)
			}
		}
	}
	return
}

func (client *RealServerClient) OnTraffic(c gnet.Conn) (action gnet.Action) {
	context0 := c.Context()
	if context0 == nil {
		return gnet.None
	}
	context := context0.(*RealServerConnContext)
	if context == nil {
		return gnet.None
	}
	nextConnCtx := context.GetNextConnCtx()
	if nextConnCtx == nil {
		return
	}
	nextConn := nextConnCtx.GetConn()
	for {
		n := c.InboundBuffered()
		if n <= 0 {
			return
		}
		buf := make([]byte, n)
		n, err := c.Read(buf)
		if err != nil {
			return gnet.Close
		}
		pkt := newDataPacket(buf)
		buf = Encode(pkt)
		err = nextConn.AsyncWrite(buf, nil)
		if err != nil {
			logging.Infof("write to next conn error", err)
		}
	}
}

func (client *RealServerClient) Dial(addr string) (gnet.Conn, error) {
	conn, err := client.gnetCli.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
