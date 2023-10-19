package crox

import (
	"container/list"
	"encoding/binary"
	"github.com/panjf2000/gnet/v2"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

type ClientProxyChannelContext struct {
	conn           gnet.Conn
	lastReadTime   int64
	nextChannelCtx *RealServerChannelContext
	mu             sync.RWMutex
}

func (ctx *ClientProxyChannelContext) SetConn(conn gnet.Conn) {
	ctx.mu.Lock()
	ctx.conn = conn
	ctx.mu.Unlock()
}

func (ctx *ClientProxyChannelContext) SetNextChannelCtx(nextChannelCtx *RealServerChannelContext) {
	ctx.mu.Lock()
	ctx.nextChannelCtx = nextChannelCtx
	ctx.mu.Unlock()
}

func (ctx *ClientProxyChannelContext) RemoveNextChannelCtx(nextChannelCtx *RealServerChannelContext) {
	ctx.mu.Lock()
	if ctx.nextChannelCtx == nextChannelCtx {
		ctx.nextChannelCtx = nil
	}
	ctx.mu.Unlock()
}

func (ctx *ClientProxyChannelContext) GetConn() gnet.Conn {
	ctx.mu.RLock()
	conn := ctx.conn
	ctx.mu.RUnlock()
	return conn
}

func (ctx *ClientProxyChannelContext) GetNextChannelCtx() *RealServerChannelContext {
	ctx.mu.RLock()
	nextChannelCtx := ctx.nextChannelCtx
	ctx.mu.RUnlock()
	return nextChannelCtx
}

type PollProxyChannelCallback func(ctx *ClientProxyChannelContext, err error)

func (client *ProxyClient) PollProxyChannel(cb PollProxyChannelCallback) {
	client.queueMu.Lock()
	if client.proxyChannelCtxQueue.Len() == 0 {
		client.queueMu.Unlock()
		go func() {
			proxyConn, err := client.proxyCli.Dial("tcp", client.proxyAddr)
			if err != nil {
				log.Printf("connect proxy server error: %v\n", err)
				cb(nil, err)
				return
			} else {
				proxyChannelCtx := &ClientProxyChannelContext{
					conn: proxyConn,
				}
				// hack method we should call SetContext() in EventHandle
				_ = proxyConn.Wake(func(c gnet.Conn, err error) error {
					c.SetContext(proxyChannelCtx)
					return nil
				})
				cb(proxyChannelCtx, nil)
			}
		}()
	} else {
		e := client.proxyChannelCtxQueue.Front()
		client.proxyChannelCtxQueue.Remove(e)
		client.queueMu.Unlock()
		proxyChannelCtx := e.Value.(*ClientProxyChannelContext)
		cb(proxyChannelCtx, nil)
	}
}

func (client *ProxyClient) OfferProxyChannel(ctx *ClientProxyChannelContext) {
	client.queueMu.Lock()
	defer client.queueMu.Unlock()
	client.proxyChannelCtxQueue.PushBack(ctx)
}

type ProxyClient struct {
	*gnet.BuiltinEventEngine
	clientId             string
	eng                  gnet.Engine
	proxyCli             *gnet.Client
	proxyAddr            string
	proxyChannelCtxQueue *list.List
	queueMu              sync.Mutex
	realServerCli        *RealServerClient
	cmdChannelCtx        *ClientProxyChannelContext
	mu                   sync.Mutex
}

func (client *ProxyClient) OnBoot(eng gnet.Engine) (action gnet.Action) {
	client.eng = eng
	return
}

func (client *ProxyClient) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	ctx := &ClientProxyChannelContext{
		conn:         c,
		lastReadTime: 0,
	}
	c.SetContext(ctx)
	return
}

func (client *ProxyClient) OnClose(c gnet.Conn, err error) (action gnet.Action) {
	if err != nil {
		log.Printf("closed by error: %v\n", err)
	}
	context := c.Context().(*ClientProxyChannelContext)
	if context == client.cmdChannelCtx {
		client.mu.Lock()
		client.cmdChannelCtx = nil
		client.mu.Unlock()
		// TODO close all real server channel
	} else {
		nextChannelCtx := context.GetNextChannelCtx()
		if nextChannelCtx != nil {
			// 解绑两个链接的一对一关系
			context.RemoveNextChannelCtx(nextChannelCtx)
			nextChannelCtx.RemoveNextChannelCtx(context)

			nextConn := nextChannelCtx.GetConn()
			closeErr := nextConn.CloseWithCallback(nil)
			if closeErr != nil {
				log.Printf("close real server channel error: %v\n", closeErr)
			}
		}
	}
	return
}

func (client *ProxyClient) OnTraffic(c gnet.Conn) (action gnet.Action) {
	ctx := c.Context().(*ClientProxyChannelContext)
	atomic.StoreInt64(&ctx.lastReadTime, time.Now().UnixMilli())
	for {
		pkt, err := Decode(c)
		if err == ErrIncompletePacket {
			break
		} else if err == ErrInvalidMagicNumber {
			log.Printf("invalid packet: %v", err)
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

func (client *ProxyClient) handleConnectMsg(c gnet.Conn, _ *ClientProxyChannelContext, pkt *packet) (action gnet.Action) {
	// TODO
	cmdChannel := c
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
			log.Printf("connect real server error: %v\n", err)
			pkt := NewDisconnectPacket(userId)
			buf := Encode(pkt)
			_ = cmdChannel.AsyncWrite(buf, nil)
			return
		}
		realServerChannelCtx := &RealServerChannelContext{
			userId: userId,
			conn:   realConn,
		}
		// 会不会因为proxy server 不停的发connect和disconnect包，导致出现并发问题
		client.PollProxyChannel(func(clientProxyChannelCtx *ClientProxyChannelContext, err error) {
			if err != nil {
				log.Printf("poll proxy channel error: %v\n", err)
				didConnectPkt := NewDisconnectPacket(userId)
				buf := Encode(didConnectPkt)
				_ = cmdChannel.AsyncWrite(buf, nil)
				return
			}
			log.Println("poll proxy channel success")
			clientProxyChannelCtx.mu.Lock()
			clientProxyChannelCtx.nextChannelCtx = realServerChannelCtx
			clientProxyChannel := clientProxyChannelCtx.conn
			clientProxyChannelCtx.mu.Unlock()

			// 发送给proxy server告诉他已经连上了real server
			connectPkt := NewProxyConnectPacket(userId, clientId)
			buf := Encode(connectPkt)
			_ = clientProxyChannel.AsyncWrite(buf, func(c gnet.Conn, err error) error {
				log.Printf("userId: %d write connect packet to proxy server success\n", userId)
				return nil
			})

			realServerChannelCtx.mu.Lock()
			realServerChannelCtx.nextChannelCtx = clientProxyChannelCtx
			realServerChannelCtx.mu.Unlock()
			log.Println("----------------------------------")
		})

		// hack method we should call SetContext() in EventHandle
		_ = realConn.Wake(func(c gnet.Conn, err error) error {
			c.SetContext(realServerChannelCtx)
			return nil
		})
	}()

	return
}

func (client *ProxyClient) handleDataMsg(_ gnet.Conn, ctx *ClientProxyChannelContext, pkt *packet) (action gnet.Action) {
	log.Printf("receive data msg from proxy server\n")
	ctx.mu.RLock()
	nextChannelCtx := ctx.nextChannelCtx
	ctx.mu.RUnlock()
	if nextChannelCtx != nil {
		nextChannelCtx.mu.RLock()
		userId := nextChannelCtx.userId
		nexConn := nextChannelCtx.conn
		nextChannelCtx.mu.RUnlock()
		err := nexConn.AsyncWrite(pkt.Data, func(c gnet.Conn, err error) error {
			if err != nil {
				log.Printf("write to %d data packet error %v\n", userId, err)
			}
			return nil
		})
		if err != nil {
			log.Printf("write to %d data packet error %v\n", userId, err)
		}
	}
	return
}

func (client *ProxyClient) handleDisconnectMsg(_ gnet.Conn, ctx *ClientProxyChannelContext, _ *packet) (action gnet.Action) {
	ctx.mu.Lock()
	nextChannelCtx := ctx.nextChannelCtx
	ctx.nextChannelCtx = nil
	ctx.mu.Unlock()
	if nextChannelCtx != nil {

		nextChannelCtx.mu.Lock()
		conn := nextChannelCtx.conn
		nextChannelCtx.nextChannelCtx = nil
		nextChannelCtx.userId = 0
		nextChannelCtx.conn = nil
		nextChannelCtx.mu.Unlock()

		_ = conn.AsyncWrite(EmptyBuf, func(c gnet.Conn, err error) error {
			c.SetContext(nil)
			_ = c.Close()
			return nil
		})
		client.OfferProxyChannel(ctx)
	}
	return
}
