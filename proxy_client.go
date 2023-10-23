package crox

import (
	"container/list"
	"crox/pkg/logging"
	"encoding/binary"
	"github.com/panjf2000/gnet/v2"
	"sync"
	"sync/atomic"
	"time"
)

type ClientProxyConnContext struct {
	conn         gnet.Conn
	lastReadTime int64
	nextConnCtx  *RealServerConnContext
	mu           sync.RWMutex
}

func (ctx *ClientProxyConnContext) SetConn(conn gnet.Conn) {
	ctx.mu.Lock()
	ctx.conn = conn
	ctx.mu.Unlock()
}

func (ctx *ClientProxyConnContext) SetNextConnCtx(nextConnCtx *RealServerConnContext) {
	ctx.mu.Lock()
	ctx.nextConnCtx = nextConnCtx
	ctx.mu.Unlock()
}

func (ctx *ClientProxyConnContext) RemoveNextConnCtx(expect *RealServerConnContext) {
	ctx.mu.Lock()
	if ctx.nextConnCtx == expect {
		ctx.nextConnCtx = nil
	}
	ctx.mu.Unlock()
}

func (ctx *ClientProxyConnContext) GetConn() gnet.Conn {
	ctx.mu.RLock()
	conn := ctx.conn
	ctx.mu.RUnlock()
	return conn
}

func (ctx *ClientProxyConnContext) GetNextConnCtx() *RealServerConnContext {
	ctx.mu.RLock()
	nextConnCtx := ctx.nextConnCtx
	ctx.mu.RUnlock()
	return nextConnCtx
}

type PollProxyConnCallback func(ctx *ClientProxyConnContext, err error)

func (client *ProxyClient) PollProxyConn(cb PollProxyConnCallback) {
	client.queueMu.Lock()
	if client.proxyConnCtxQueue.Len() == 0 {
		client.queueMu.Unlock()
		go func() {
			proxyConn, err := client.proxyCli.Dial("tcp", client.proxyAddr)
			if err != nil {
				logging.Infof("connect proxy server error: %v", err)
				cb(nil, err)
				return
			} else {
				proxyConnCtx := &ClientProxyConnContext{
					conn: proxyConn,
				}
				// hack method we should call SetContext() in EventHandle
				//_ = proxyConn.Wake(func(c gnet.Conn, err error) error {
				//	c.SetContext(proxyConnCtx)
				//	return nil
				//})
				cb(proxyConnCtx, nil)
			}
		}()
	} else {
		e := client.proxyConnCtxQueue.Front()
		client.proxyConnCtxQueue.Remove(e)
		client.queueMu.Unlock()
		proxyConnCtx := e.Value.(*ClientProxyConnContext)
		cb(proxyConnCtx, nil)
	}
}

func (client *ProxyClient) OfferProxyConn(ctx *ClientProxyConnContext) {
	client.queueMu.Lock()
	defer client.queueMu.Unlock()
	client.proxyConnCtxQueue.PushBack(ctx)
}

type ProxyClient struct {
	*gnet.BuiltinEventEngine
	clientId          string
	eng               gnet.Engine
	proxyCli          *gnet.Client
	proxyAddr         string
	proxyConnCtxQueue *list.List
	queueMu           sync.Mutex
	realServerCli     *RealServerClient
	cmdConnCtx        *ClientProxyConnContext
	mu                sync.Mutex
}

func (client *ProxyClient) OnBoot(eng gnet.Engine) (action gnet.Action) {
	client.eng = eng
	return
}

func (client *ProxyClient) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	ctx := &ClientProxyConnContext{
		conn:         c,
		lastReadTime: 0,
	}
	c.SetContext(ctx)
	return
}

func (client *ProxyClient) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	if err != nil {
		logging.Infof("closed by error: %v", err)
	}
	context := c.Context().(*ClientProxyConnContext)
	if context == client.cmdConnCtx {
		client.mu.Lock()
		client.cmdConnCtx = nil
		client.mu.Unlock()
		// TODO close all real server conn

		// TODO reconnect to proxy server
	} else {
		nextConnCtx := context.GetNextConnCtx()
		if nextConnCtx != nil {
			// 解绑两个链接的一对一关系
			context.RemoveNextConnCtx(nextConnCtx)
			nextConnCtx.RemoveNextConnCtx(context)

			nextConn := nextConnCtx.GetConn()
			closeErr := nextConn.CloseWithCallback(nil)
			if closeErr != nil {
				logging.Infof("close real server conn error: %v", closeErr)
			}
		}
	}
	return
}

func (client *ProxyClient) OnTraffic(c gnet.Conn) (action gnet.Action) {
	ctx := c.Context().(*ClientProxyConnContext)
	atomic.StoreInt64(&ctx.lastReadTime, time.Now().UnixMilli())
	for {
		pkt, err := Decode(c)
		if err == ErrIncompletePacket {
			break
		} else if err == ErrInvalidMagicNumber {
			logging.Infof("invalid packet: %v", err)
			return gnet.Close
		}
		switch pkt.Type {
		case PktTypeConnect:
			action = client.handleConnectMsg(c, ctx, pkt)
			if action != gnet.None {
				return
			}
		case PktTypeData:
			action = client.handleDataMsg(c, ctx, pkt)
			if action != gnet.None {
				return
			}
		case PktTypeDisconnect:
			action = client.handleDisconnectMsg(c, ctx, pkt)
			if action != gnet.None {
				return
			}
		}
	}
	return
}

func (client *ProxyClient) handleConnectMsg(c gnet.Conn, _ *ClientProxyConnContext, pkt *packet) (action gnet.Action) {
	// TODO
	cmdConn := c
	data := pkt.Data
	clientId := client.clientId
	userId := binary.LittleEndian.Uint64(data[0:8])
	lanLen := binary.LittleEndian.Uint32(data[8:12])
	// lan:192.168.1.123:8912
	lan := string(data[12 : 12+lanLen])
	// connect to real server
	realServerCli := client.realServerCli
	go func() {
		realConn, err := realServerCli.Dial(lan)
		if err != nil {
			logging.Infof("connect real server error: %v", err)
			pkt := NewDisconnectPacket(userId)
			buf := Encode(pkt)
			_ = cmdConn.AsyncWrite(buf, nil)
			return
		}
		realServerConnCtx := &RealServerConnContext{
			userId: userId,
			conn:   realConn,
		}
		// 会不会因为proxy server 不停的发connect和disconnect包，导致出现并发问题
		client.PollProxyConn(func(clientProxyConnCtx *ClientProxyConnContext, err error) {
			if err != nil {
				logging.Infof("poll proxy conn error: %v", err)
				disconnectPkt := NewDisconnectPacket(userId)
				buf := Encode(disconnectPkt)
				_ = cmdConn.AsyncWrite(buf, nil)
				return
			}
			logging.Infof("poll proxy conn success")
			clientProxyConnCtx.mu.Lock()
			clientProxyConnCtx.nextConnCtx = realServerConnCtx
			clientProxyConn := clientProxyConnCtx.conn
			clientProxyConnCtx.mu.Unlock()

			// 发送给proxy server告诉他已经连上了real server
			connectPkt := NewProxyConnectPacket(userId, clientId)
			buf := Encode(connectPkt)
			_ = clientProxyConn.AsyncWrite(buf, func(c gnet.Conn, err error) error {
				logging.Infof("userId: %d write connect packet to proxy server success", userId)
				c.SetContext(clientProxyConnCtx)
				return nil
			})

			realServerConnCtx.mu.Lock()
			realServerConnCtx.nextConnCtx = clientProxyConnCtx
			realServerConnCtx.mu.Unlock()
		})

		// hack method we should call SetContext() in EventHandle
		_ = realConn.Wake(func(c gnet.Conn, err error) error {
			c.SetContext(realServerConnCtx)
			return nil
		})
	}()

	return
}

func (client *ProxyClient) handleDataMsg(_ gnet.Conn, ctx *ClientProxyConnContext, pkt *packet) (action gnet.Action) {
	logging.Infof("receive data msg from proxy server")
	ctx.mu.RLock()
	nextConnCtx := ctx.nextConnCtx
	ctx.mu.RUnlock()
	if nextConnCtx != nil {
		nextConnCtx.mu.RLock()
		userId := nextConnCtx.userId
		nexConn := nextConnCtx.conn
		nextConnCtx.mu.RUnlock()
		err := nexConn.AsyncWrite(pkt.Data, func(c gnet.Conn, err error) error {
			if err != nil {
				logging.Infof("write to %d data packet error %v", userId, err)
			}
			return nil
		})
		if err != nil {
			logging.Infof("write to %d data packet error %v", userId, err)
		}
	}
	return
}

func (client *ProxyClient) handleDisconnectMsg(_ gnet.Conn, ctx *ClientProxyConnContext, _ *packet) (action gnet.Action) {
	ctx.mu.Lock()
	nextConnCtx := ctx.nextConnCtx
	ctx.nextConnCtx = nil
	ctx.mu.Unlock()
	if nextConnCtx != nil {

		nextConnCtx.mu.Lock()
		conn := nextConnCtx.conn
		nextConnCtx.nextConnCtx = nil
		nextConnCtx.userId = 0
		nextConnCtx.conn = nil
		nextConnCtx.mu.Unlock()

		_ = conn.Wake(func(c gnet.Conn, err error) error {
			c.SetContext(nil)
			_ = c.Close()
			return nil
		})
		client.OfferProxyConn(ctx)
	}
	return
}
