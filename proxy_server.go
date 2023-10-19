package crox

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/panjf2000/gnet/v2"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

var EmptyBuf = make([]byte, 0)

// ProxyChannelContext cmdChannel和transferChannel共用同一个ChannelContext类型，因为他们都是连接同一个server的相同端口，只是传输的数据不一样
type ProxyChannelContext struct {
	channelType       int // 0: cmdChannel, 1: transferChannel
	conn              gnet.Conn
	lastReadTime      int64
	heartbeatSeq      uint64
	clientId          string
	userChannelCtxMap sync.Map            // cmdChannel 的属性
	userId            uint64              // transferChannel 的属性
	nextChannelCtx    *UserChannelContext // transferChannel 的属性
	mu                sync.RWMutex
}

func (ctx *ProxyChannelContext) SetNextChannelCtx(nextChannelCtx *UserChannelContext) {
	ctx.mu.Lock()
	ctx.nextChannelCtx = nextChannelCtx
	ctx.mu.Unlock()
}

func (ctx *ProxyChannelContext) GetNextChannelCtx() *UserChannelContext {
	ctx.mu.RLock()
	nextChannelCtx := ctx.nextChannelCtx
	ctx.mu.RUnlock()
	return nextChannelCtx
}

func (ctx *ProxyChannelContext) SetConn(conn gnet.Conn) {
	ctx.mu.Lock()
	ctx.conn = conn
	ctx.mu.Unlock()
}

func (ctx *ProxyChannelContext) GetConn() gnet.Conn {
	ctx.mu.RLock()
	conn := ctx.conn
	ctx.mu.RUnlock()
	return conn
}

// ProxyServer 需要接受ProxyClient的连接，还需要接受外来需要转发的连接
type ProxyServer struct {
	gnet.BuiltinEventEngine
	eng                  gnet.Engine
	network              string
	addr                 string
	connected            int32
	portCmdChannelCtxMap sync.Map
	cmdChannelCtxMap     sync.Map
	cfg                  *ProxyConfig
}

func (s *ProxyServer) Start(cancelFunc context.CancelFunc) {
	go func() {
		err := gnet.Run(s, fmt.Sprintf("%s://%s", s.network, s.addr), gnet.WithMulticore(true), gnet.WithReusePort(true))
		if err != nil {
			cancelFunc()
			log.Fatalf("start server error %v\n", err)
		}
	}()
}

func (s *ProxyServer) OnBoot(eng gnet.Engine) (action gnet.Action) {
	// eng.Stop(context.Background())
	log.Printf("running proxy server on %s\n", fmt.Sprintf("%s://%s", s.network, s.addr))
	s.eng = eng
	return
}

func (s *ProxyServer) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	atomic.AddInt32(&s.connected, 1)
	ctx := &ProxyChannelContext{
		channelType:  -1, // 为定义的channelType
		conn:         c,
		lastReadTime: time.Now().UnixMilli(),
		heartbeatSeq: uint64(rand.Intn(1000000)),
		clientId:     "",
	}
	c.SetContext(ctx)
	// start heartbeat check
	return
}

func (s *ProxyServer) OnTraffic(c gnet.Conn) (action gnet.Action) {
	ctx := c.Context().(*ProxyChannelContext)
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
		case PktTypeAuth:
			action = s.handleAuthMsg(c, ctx, pkt)
			if action != gnet.None {
				return
			}
		case PktTypeConnect:
			action = s.handleConnectMsg(c, ctx, pkt)
			if action != gnet.None {
				return
			}
		case PktTypeData:
			action = s.handleDataMsg(c, ctx, pkt)
			if action != gnet.None {
				return
			}
		case PktTypeHeartbeat:
			action = s.handleHeartbeatMsg(c, ctx, pkt)
			if action != gnet.None {
				return
			}
		case PktTypeDisconnect:
			action = s.handleDisconnectMsg(c, ctx, pkt)
			if action != gnet.None {
				return
			}
		}
	}
	return
}

// |clientId|
func (s *ProxyServer) handleAuthMsg(_ gnet.Conn, ctx *ProxyChannelContext, pkt *packet) gnet.Action {
	data := pkt.Data
	clientId := string(data)

	log.Printf("receive auth msg from client %s\n", clientId)

	ctx.mu.Lock()
	ctx.clientId = clientId
	ctx.channelType = 0
	ctx.mu.Unlock()

	ports := s.cfg.GetClientInetPorts(clientId)
	for _, port := range ports {
		// 这个映射是代理服务器对外开了哪些端口，对应的使用哪个客户端连上来的tcp连接来发送指令到客户端
		s.portCmdChannelCtxMap.Store(port, ctx)
	}
	s.cmdChannelCtxMap.Store(clientId, ctx)
	return gnet.None
}

// |userId:8|clientIdLen:4|clientId:clientIdLen|
func (s *ProxyServer) handleConnectMsg(_ gnet.Conn, ctx *ProxyChannelContext, pkt *packet) gnet.Action {
	log.Printf("receive connect msg from client\n")
	ctx.mu.Lock()
	ctx.channelType = 1
	ctx.mu.Unlock()

	data := pkt.Data
	userId := binary.LittleEndian.Uint64(data[:8])
	clientIdLen := binary.LittleEndian.Uint32(data[8:12])
	clientId := string(data[12 : 12+clientIdLen])

	log.Printf("receive connect success msg from client %s, userId %d\n", clientId, userId)

	cmdChannel, ok := s.cmdChannelCtxMap.Load(clientId)
	if !ok {
		log.Printf("client %s not found\n", clientId)
		return gnet.Close
	}
	// 这里会不会有并发问题啊？两个链接同时连上来，发了相同的userId，那么就会在两个协程里共同操作这个userChannel
	userChannelCtx0, ok := cmdChannel.(*ProxyChannelContext).userChannelCtxMap.Load(userId)
	if ok {
		userChannelCtx := userChannelCtx0.(*UserChannelContext)
		ctx.mu.Lock()
		ctx.clientId = clientId
		ctx.userId = userId
		ctx.nextChannelCtx = userChannelCtx
		ctx.mu.Unlock()

		userChannelCtx.mu.Lock()
		userChannelCtx.nextChannelCtx = ctx
		userChannelCtx.mu.Unlock()
	} else {
		log.Printf("user %d not found\n", userId)
	}
	return gnet.None
}

// |data|
func (s *ProxyServer) handleDataMsg(_ gnet.Conn, ctx *ProxyChannelContext, pkt *packet) gnet.Action {
	log.Printf("receive data msg from channel, channel type %d\n", ctx.channelType)
	data := pkt.Data
	ctx.mu.RLock()
	nextChannelCtx := ctx.nextChannelCtx
	ctx.mu.RUnlock()
	if nextChannelCtx != nil {

		nextChannelCtx.mu.RLock()
		nexConn := nextChannelCtx.conn
		nextChannelCtx.mu.RUnlock()

		err := nexConn.AsyncWrite(data, nil)
		if err != nil {
			log.Printf("write data packet error %v\n", err)
			_ = nexConn.CloseWithCallback(nil)
		}
	}
	return gnet.None
}

// |seq:8|
func (s *ProxyServer) handleHeartbeatMsg(c gnet.Conn, _ *ProxyChannelContext, pkt *packet) gnet.Action {
	data := pkt.Data
	seq := binary.LittleEndian.Uint64(data)
	log.Printf("receive heartbeat seq %d\n", seq)
	heartbeatPacket := NewHeartbeatPacket(seq)
	buf := Encode(heartbeatPacket)
	_, err := c.Write(buf)
	if err != nil {
		log.Printf("write heartbeat packet error %v\n", err)
		return gnet.Close
	}
	return gnet.None
}

// |userId:8|
func (s *ProxyServer) handleDisconnectMsg(c gnet.Conn, ctx *ProxyChannelContext, pkt *packet) (action gnet.Action) {
	ctx.mu.RLock()
	clientId := ctx.clientId
	channelType := ctx.channelType
	userId := ctx.userId
	ctx.mu.RUnlock()
	data := pkt.Data
	log.Printf("receive disconnect msg from channel, channel type %d\n", ctx.channelType)
	if channelType == -1 {
		// invalid channel
		return gnet.Close
	}
	// 代理连接没有连上服务器由控制连接发送用户端断开连接消息
	if channelType == 0 {
		userId := binary.LittleEndian.Uint64(data)
		userChannelCtx0, ok := ctx.userChannelCtxMap.Load(userId)
		if ok {
			userChannelCtx := userChannelCtx0.(*UserChannelContext)

			userChannelCtx.mu.RLock()
			userChannel := userChannelCtx.conn
			userChannelCtx.mu.RUnlock()

			ctx.userChannelCtxMap.Delete(userId)
			// Flush and close the connection immediately when the last message of the server has been sent.
			err := userChannel.AsyncWrite(EmptyBuf, func(c gnet.Conn, err error) error {
				_ = c.Flush()
				_ = c.Close()
				return nil
			})
			if err != nil {
				log.Fatalf("write disconnect packet error %v\n", err)
			}
		}
		return gnet.None
	}
	cmdChannelCtx0, ok := s.cmdChannelCtxMap.Load(clientId)
	if !ok {
		log.Fatalf("client %s not found\n", clientId)
		return gnet.None
	}
	// 从用户连接发送上来的断开连接消息
	cmdChannelCtx := cmdChannelCtx0.(*ProxyChannelContext)
	cmdChannelCtx.userChannelCtxMap.Delete(userId)

	userChannelCtx := ctx.GetNextChannelCtx()
	userChannel := userChannelCtx.GetConn()

	ctx.mu.Lock()
	ctx.nextChannelCtx = nil
	ctx.userId = 0
	ctx.clientId = ""
	ctx.mu.Unlock()

	// Flush and close the connection immediately when the last message of the server has been sent.
	err := userChannel.Wake(func(c gnet.Conn, err error) error {
		_ = c.Flush()
		_ = c.CloseWithCallback(func(c gnet.Conn, err error) error {
			log.Printf("user channel close, userId %d\n", userId)
			return nil
		})
		return nil
	})
	if err != nil {
		log.Fatalf("write disconnect packet error %v\n", err)
	}
	return gnet.None

}

func startHeartbeatCheck(ctx *ProxyChannelContext) {
	// 每隔一段时间检查一下心跳包的状态，如果超过一定时间没有收到心跳包，就断开连接
	readTicker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-readTicker.C:
			// load ctx.lastReadTime
			lastReadTime := atomic.LoadInt64(&ctx.lastReadTime)
			// 检查ctx.lastReadTime是否超过一定时间，如果超过一定时间，就断开连接
			if time.Now().UnixMilli()-lastReadTime > 30*1000 {
				log.Printf("heartbeat check timeout, last read time %d\n", lastReadTime)
				_ = ctx.conn.CloseWithCallback(nil)
				return
			}
		}
	}
}

func startSendHeartbeat(ctx *ProxyChannelContext) {
	writeTicker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-writeTicker.C:
			// 发送心跳包
			heartbeatPacket := NewHeartbeatPacket(atomic.AddUint64(&ctx.heartbeatSeq, 1))
			buf := Encode(heartbeatPacket)
			err := ctx.conn.AsyncWrite(buf, nil)
			if err != nil {
				log.Printf("write heartbeat packet error %v\n", err)
				return
			}
		}
	}
}
