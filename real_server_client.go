package crox

import (
	"github.com/panjf2000/gnet/v2"
	"log"
	"sync"
)

type RealServerChannelContext struct {
	userId         uint64
	conn           gnet.Conn
	nextChannelCtx *ClientProxyChannelContext
	mu             sync.RWMutex
}

func (ctx *RealServerChannelContext) GetUserId() uint64 {
	ctx.mu.RLock()
	userId := ctx.userId
	ctx.mu.RUnlock()
	return userId
}

func (ctx *RealServerChannelContext) GetNextChannelCtx() *ClientProxyChannelContext {
	ctx.mu.RLock()
	nextChannelCtx := ctx.nextChannelCtx
	ctx.mu.RUnlock()
	return nextChannelCtx
}

func (ctx *RealServerChannelContext) SetNextChannelCtx(nextChannelCtx *ClientProxyChannelContext) {
	ctx.mu.Lock()
	ctx.nextChannelCtx = nextChannelCtx
	ctx.mu.Unlock()
}

func (ctx *RealServerChannelContext) RemoveNextChannelCtx(nextChannelCtx *ClientProxyChannelContext) {
	ctx.mu.Lock()
	if ctx.nextChannelCtx == nextChannelCtx {
		ctx.nextChannelCtx = nil
	}
	ctx.mu.Unlock()
}

func (ctx *RealServerChannelContext) GetConn() gnet.Conn {
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
	context := c.Context().(*RealServerChannelContext)
	if context != nil {
		nextChannelCtx := context.GetNextChannelCtx()
		if nextChannelCtx != nil {

			context.RemoveNextChannelCtx(nextChannelCtx)
			nextChannelCtx.RemoveNextChannelCtx(context)

			conn := nextChannelCtx.GetConn()
			pkt := NewDisconnectPacket(context.GetUserId())
			buf := Encode(pkt)
			writeErr := conn.AsyncWrite(buf, nil)
			if writeErr != nil {
				log.Println("write to next channel error", writeErr)
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
	context := context0.(*RealServerChannelContext)
	if context == nil {
		return gnet.None
	}
	nextChannelCtx := context.GetNextChannelCtx()
	if nextChannelCtx == nil {
		return
	}
	nextChannel := nextChannelCtx.GetConn()
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
		pkt := NewDataPacket(buf)
		buf = Encode(pkt)
		err = nextChannel.AsyncWrite(buf, nil)
		if err != nil {
			log.Println("write to next channel error", err)
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
