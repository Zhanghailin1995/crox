package crox

import (
	"context"
	"crox/pkg/logging"
	"encoding/binary"
	"fmt"
	"github.com/panjf2000/gnet/v2"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// ProxyConnContext cmdConn和transferConn共用同一个ConnContext类型，因为他们都是连接同一个server的相同端口，只是传输的数据不一样
type ProxyConnContext struct {
	connType       int // 0: cmdConn, 1: transferConn
	conn           gnet.Conn
	lastReadTime   int64
	heartbeatSeq   uint64
	clientId       string
	userConnCtxMap sync.Map         // cmdConn 的属性
	userId         uint64           // transferConn 的属性
	nextConnCtx    *UserConnContext // transferConn 的属性
	mu             sync.RWMutex
}

func (ctx *ProxyConnContext) SetNextConnCtx(nextConnCtx *UserConnContext) {
	ctx.mu.Lock()
	ctx.nextConnCtx = nextConnCtx
	ctx.mu.Unlock()
}

func (ctx *ProxyConnContext) GetNextConnCtx() *UserConnContext {
	ctx.mu.RLock()
	nextConnCtx := ctx.nextConnCtx
	ctx.mu.RUnlock()
	return nextConnCtx
}

func (ctx *ProxyConnContext) SetConn(conn gnet.Conn) {
	ctx.mu.Lock()
	ctx.conn = conn
	ctx.mu.Unlock()
}

func (ctx *ProxyConnContext) GetConn() gnet.Conn {
	ctx.mu.RLock()
	conn := ctx.conn
	ctx.mu.RUnlock()
	return conn
}

// ProxyServer 需要接受ProxyClient的连接，还需要接受外来需要转发的连接
type ProxyServer struct {
	gnet.BuiltinEventEngine
	eng               gnet.Engine
	network           string
	addr              string
	connected         int32
	portCmdConnCtxMap sync.Map
	cmdConnCtxMap     sync.Map
	cfg               *ProxyConfig
}

func (s *ProxyServer) Start(cancelFunc context.CancelFunc) {
	go func() {
		err := gnet.Run(s, fmt.Sprintf("%s://%s", s.network, s.addr), gnet.WithMulticore(true), gnet.WithReusePort(true))
		if err != nil {
			cancelFunc()
			log.Fatalf("start server error %v", err)
		}
	}()
}

func (s *ProxyServer) OnBoot(eng gnet.Engine) (action gnet.Action) {
	// eng.Stop(context.Background())
	logging.Infof("running proxy server on %s", fmt.Sprintf("%s://%s", s.network, s.addr))
	s.eng = eng
	return
}

func (s *ProxyServer) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	atomic.AddInt32(&s.connected, 1)
	ctx := &ProxyConnContext{
		connType:     -1, // 为定义的connType
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
	ctx := c.Context().(*ProxyConnContext)
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
	return gnet.None
}

// |clientId|
func (s *ProxyServer) handleAuthMsg(_ gnet.Conn, ctx *ProxyConnContext, pkt *packet) gnet.Action {
	data := pkt.Data
	clientId := string(data)

	logging.Infof("receive auth msg from client %s", clientId)

	ctx.mu.Lock()
	ctx.clientId = clientId
	ctx.connType = 0
	ctx.mu.Unlock()

	ports := s.cfg.GetClientInetPorts(clientId)
	for _, port := range ports {
		// 这个映射是代理服务器对外开了哪些端口，对应的使用哪个客户端连上来的tcp连接来发送指令到客户端
		s.portCmdConnCtxMap.Store(port, ctx)
	}
	s.cmdConnCtxMap.Store(clientId, ctx)
	return gnet.None
}

// |userId:8|clientIdLen:4|clientId:clientIdLen|
func (s *ProxyServer) handleConnectMsg(_ gnet.Conn, ctx *ProxyConnContext, pkt *packet) gnet.Action {
	logging.Infof("receive connect msg from client")
	ctx.mu.Lock()
	ctx.connType = 1
	ctx.mu.Unlock()

	data := pkt.Data
	userId := binary.LittleEndian.Uint64(data[:8])
	clientIdLen := binary.LittleEndian.Uint32(data[8:12])
	clientId := string(data[12 : 12+clientIdLen])

	logging.Infof("receive connect success msg from client %s, userId %d", clientId, userId)

	cmdConn, ok := s.cmdConnCtxMap.Load(clientId)
	if !ok {
		logging.Infof("client %s not found", clientId)
		return gnet.Close
	}
	// 这里会不会有并发问题啊？两个链接同时连上来，发了相同的userId，那么就会在两个协程里共同操作这个userConn
	userConnCtx0, ok := cmdConn.(*ProxyConnContext).userConnCtxMap.Load(userId)
	if ok {
		userConnCtx := userConnCtx0.(*UserConnContext)
		ctx.mu.Lock()
		ctx.clientId = clientId
		ctx.userId = userId
		ctx.nextConnCtx = userConnCtx
		ctx.mu.Unlock()

		userConnCtx.mu.Lock()
		userConnCtx.nextConnCtx = ctx
		userConnCtx.mu.Unlock()
	} else {
		logging.Infof("user %d not found", userId)
	}
	return gnet.None
}

// |data|
func (s *ProxyServer) handleDataMsg(_ gnet.Conn, ctx *ProxyConnContext, pkt *packet) gnet.Action {
	logging.Infof("receive data msg from conn, conn type %d", ctx.connType)
	data := pkt.Data
	ctx.mu.RLock()
	nextConnCtx := ctx.nextConnCtx
	ctx.mu.RUnlock()
	if nextConnCtx != nil {

		nextConnCtx.mu.RLock()
		nexConn := nextConnCtx.conn
		nextConnCtx.mu.RUnlock()

		err := nexConn.AsyncWrite(data, nil)
		if err != nil {
			logging.Infof("write data packet error %v", err)
			_ = nexConn.CloseWithCallback(nil)
		}
	}
	return gnet.None
}

// |seq:8|
func (s *ProxyServer) handleHeartbeatMsg(c gnet.Conn, _ *ProxyConnContext, pkt *packet) gnet.Action {
	data := pkt.Data
	seq := binary.LittleEndian.Uint64(data)
	logging.Infof("receive heartbeat seq %d", seq)
	heartbeatPacket := NewHeartbeatPacket(seq)
	buf := Encode(heartbeatPacket)
	_, err := c.Write(buf)
	if err != nil {
		logging.Infof("write heartbeat packet error %v", err)
		return gnet.Close
	}
	return gnet.None
}

// |userId:8|
func (s *ProxyServer) handleDisconnectMsg(c gnet.Conn, ctx *ProxyConnContext, pkt *packet) (action gnet.Action) {
	ctx.mu.RLock()
	clientId := ctx.clientId
	connType := ctx.connType
	userId := ctx.userId
	ctx.mu.RUnlock()
	data := pkt.Data
	logging.Infof("receive disconnect msg from conn, conn type %d", ctx.connType)
	if connType == -1 {
		// invalid conn
		return gnet.Close
	}
	// 代理连接没有连上服务器由控制连接发送用户端断开连接消息
	if connType == 0 {
		userId := binary.LittleEndian.Uint64(data)
		userConnCtx0, ok := ctx.userConnCtxMap.Load(userId)
		if ok {
			userConnCtx := userConnCtx0.(*UserConnContext)

			userConn := userConnCtx.GetConn()

			ctx.userConnCtxMap.Delete(userId)
			// Flush and close the connection immediately when the last message of the server has been sent.
			err := userConn.Wake(func(c gnet.Conn, err error) error {
				_ = c.Flush()
				_ = c.Close()
				return nil
			})
			if err != nil {
				log.Fatalf("write disconnect packet error %v", err)
			}
		}
		return gnet.None
	}
	cmdConnCtx0, ok := s.cmdConnCtxMap.Load(clientId)
	if !ok {
		log.Fatalf("client %s not found", clientId)
		return gnet.None
	}
	// 从用户连接发送上来的断开连接消息
	cmdConnCtx := cmdConnCtx0.(*ProxyConnContext)
	cmdConnCtx.userConnCtxMap.Delete(userId)

	userConnCtx := ctx.GetNextConnCtx()
	userConn := userConnCtx.GetConn()

	ctx.mu.Lock()
	ctx.nextConnCtx = nil
	ctx.userId = 0
	ctx.clientId = ""
	ctx.mu.Unlock()

	// Flush and close the connection immediately when the last message of the server has been sent.
	err := userConn.Wake(func(c gnet.Conn, err error) error {
		_ = c.Flush()
		_ = c.CloseWithCallback(func(c gnet.Conn, err error) error {
			logging.Infof("user conn close, userId %d", userId)
			return nil
		})
		return nil
	})
	if err != nil {
		log.Fatalf("write disconnect packet error %v", err)
	}
	return gnet.None

}

func startHeartbeatCheck(ctx *ProxyConnContext) {
	// 每隔一段时间检查一下心跳包的状态，如果超过一定时间没有收到心跳包，就断开连接
	readTicker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-readTicker.C:
			// load ctx.lastReadTime
			lastReadTime := atomic.LoadInt64(&ctx.lastReadTime)
			// 检查ctx.lastReadTime是否超过一定时间，如果超过一定时间，就断开连接
			if time.Now().UnixMilli()-lastReadTime > 30*1000 {
				logging.Infof("heartbeat check timeout, last read time %d", lastReadTime)
				_ = ctx.conn.CloseWithCallback(nil)
				return
			}
		}
	}
}

func startSendHeartbeat(ctx *ProxyConnContext) {
	writeTicker := time.NewTicker(10 * time.Second)
	for {
		select {
		case <-writeTicker.C:
			// 发送心跳包
			heartbeatPacket := NewHeartbeatPacket(atomic.AddUint64(&ctx.heartbeatSeq, 1))
			buf := Encode(heartbeatPacket)
			err := ctx.conn.AsyncWrite(buf, nil)
			if err != nil {
				logging.Infof("write heartbeat packet error %v", err)
				return
			}
		}
	}
}
