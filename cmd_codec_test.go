package crox

import (
	"encoding/binary"
	"errors"
	"github.com/panjf2000/gnet/v2"
	"io"
	"net"
	"testing"
	"time"
)

var ErrUnsupportedOperation = errors.New("unsupported operation")

type TestConn struct {
	buffer []byte
}

func (c *TestConn) Read(p []byte) (n int, err error) {
	if len(c.buffer) == 0 {
		return 0, io.EOF
	}
	n = copy(p, c.buffer)
	c.buffer = c.buffer[n:]
	return n, nil
}

func (c *TestConn) WriteTo(w io.Writer) (n int64, err error) {
	bytesWritten, err := w.Write(c.buffer)
	c.buffer = c.buffer[bytesWritten:]
	return int64(bytesWritten), err
}

func (c *TestConn) Next(n int) (buf []byte, err error) {
	if n > len(c.buffer) {
		n = len(c.buffer)
	}
	buf = make([]byte, n)
	copy(buf, c.buffer)
	c.buffer = c.buffer[n:]
	return buf, nil
}

func (c *TestConn) Peek(n int) (buf []byte, err error) {
	if n > len(c.buffer) {
		return nil, io.ErrShortBuffer
	}
	buf = make([]byte, n)
	copy(buf, c.buffer[:n])
	return buf, nil
}

func (c *TestConn) Discard(n int) (discarded int, err error) {
	if n > len(c.buffer) {
		return 0, io.ErrShortBuffer
	}
	discarded = n
	c.buffer = c.buffer[n:]
	return discarded, nil
}

func (c *TestConn) Write(p []byte) (n int, err error) {
	c.buffer = append(c.buffer, p...)
	return len(p), nil
}

func (c *TestConn) Writev(bs [][]byte) (n int, err error) {
	for _, b := range bs {
		c.buffer = append(c.buffer, b...)
		n += len(b)
	}
	return n, nil
}

func (c *TestConn) Flush() (err error) {
	// 在这里实现将缓冲区中的数据刷新到底层连接的逻辑
	// 如果底层连接是同步的，可以在这里执行实际的写入操作
	// 如果底层连接是异步的，可以在这里将数据传递给异步写入的队列或通道
	// 返回可能的错误
	return nil
}

func (c *TestConn) OutboundBuffered() (n int) {
	return len(c.buffer)
}

func (c *TestConn) AsyncWrite([]byte, gnet.AsyncCallback) (err error) {
	// 在这里实现将数据异步写入到底层连接的逻辑
	// 可以在这里启动一个新的 goroutine 来执行写入操作
	// 在写入完成后，调用回调函数来通知写入结果
	// 返回可能的错误
	return nil
}

func (c *TestConn) AsyncWritev([][]byte, gnet.AsyncCallback) (err error) {
	// 在这里实现将多个数据片段异步写入到底层连接的逻辑
	// 可以在这里启动一个新的 goroutine 来执行写入操作
	// 在写入完成后，调用回调函数来通知写入结果
	// 返回可能的错误
	return nil
}

func (c *TestConn) ReadFrom(io.Reader) (n int64, err error) {
	// 在这里实现从底层连接读取数据的逻辑
	// 返回可能的错误
	return 0, nil
}

func (c *TestConn) InboundBuffered() (n int) {
	return len(c.buffer)
}

func (c *TestConn) Context() (ctx interface{}) {
	return ErrUnsupportedOperation
}

func (c *TestConn) SetContext(_ interface{}) {

}

func (c *TestConn) LocalAddr() (addr net.Addr) {
	return nil
}

func (c *TestConn) RemoteAddr() (addr net.Addr) {
	return nil
}

func (c *TestConn) Wake(_ gnet.AsyncCallback) (err error) {
	return nil
}

func (c *TestConn) CloseWithCallback(_ gnet.AsyncCallback) (err error) {
	return nil
}

func (c *TestConn) Close() error {
	return nil
}

func (c *TestConn) SetDeadline(time.Time) (err error) {
	return nil
}

func (c *TestConn) SetReadDeadline(time.Time) (err error) {
	return nil
}

func (c *TestConn) SetWriteDeadline(time.Time) (err error) {
	return nil
}

func (c *TestConn) Fd() int {
	return 0
}

func (c *TestConn) Dup() (int, error) {
	return 0, nil
}

func (c *TestConn) SetReadBuffer(int) error {
	return nil
}

func (c *TestConn) SetWriteBuffer(int) error {
	return nil
}

func (c *TestConn) SetLinger(int) error {
	return nil
}

func (c *TestConn) SetKeepAlivePeriod(time.Duration) error {
	return nil
}

func (c *TestConn) SetNoDelay(bool) error {
	return nil
}

func TestDecode(t *testing.T) {
	buf := make([]byte, packetHeaderSize)
	binary.LittleEndian.PutUint16(buf[:2], packetMagic)
	// put pkt type
	buf[2] = PktTypeAuth
	// put pkt version
	buf[3] = 0x01
	// put pkt size
	clientId := "testclientid1"
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(clientId)))
	// put data
	buf = append(buf, []byte(clientId)...)
	conn := &TestConn{
		buffer: buf,
	}
	pkt, err := decode(conn)
	// check err
	if err != nil {
		t.Errorf("err = %s, want nil", err)
	}
	// check pkt
	if pkt.Magic != packetMagic {
		t.Errorf("pkt.Magic = %d, want %d", pkt.Magic, packetMagic)
	}
	if pkt.Type != PktTypeAuth {
		t.Errorf("pkt.Type = %d, want %d", pkt.Type, PktTypeAuth)
	}
	if pkt.Version != 0x01 {
		t.Errorf("pkt.Version = %d, want %d", pkt.Version, 0)
	}
	if pkt.Size != uint32(len(clientId)) {
		t.Errorf("pkt.Size = %d, want %d", pkt.Size, len(clientId))
	}
	if string(pkt.Data) != clientId {
		t.Errorf("pkt.Data = %s, want %s", pkt.Data, clientId)
	}
}

func TestNewTransferPacket(t *testing.T) {
	data := []byte("transfer packet")
	pkt := newDataPacket(data)
	// check pkt
	if pkt.Magic != packetMagic {
		t.Errorf("pkt.Magic = %d, want %d", pkt.Magic, packetMagic)
	}
	if pkt.Type != PktTypeData {
		t.Errorf("pkt.Type = %d, want %d", pkt.Type, PktTypeData)
	}
	if pkt.Version != 0x01 {
		t.Errorf("pkt.Version = %d, want %d", pkt.Version, 0)
	}
	if pkt.Size != uint32(len(data)) {
		t.Errorf("pkt.Size = %d, want %d", pkt.Size, len(data))
	}
	if string(pkt.Data) != string(data) {
		t.Errorf("pkt.Data = %s, want %s", pkt.Data, data)
	}
}

func TestNewHeartbeatPacket(t *testing.T) {
	seq := uint64(1234567890)
	pkt := newHeartbeatPacket(seq)
	// check pkt
	if pkt.Magic != packetMagic {
		t.Errorf("pkt.Magic = %d, want %d", pkt.Magic, packetMagic)
	}
	if pkt.Type != PktTypeHeartbeat {
		t.Errorf("pkt.Type = %d, want %d", pkt.Type, PktTypeHeartbeat)
	}
	if pkt.Version != 0x01 {
		t.Errorf("pkt.Version = %d, want %d", pkt.Version, 0)
	}
	if pkt.Size != 8 {
		t.Errorf("pkt.Size = %d, want %d", pkt.Size, 8)
	}
	if binary.LittleEndian.Uint64(pkt.Data) != seq {
		t.Errorf("pkt.Data = %d, want %d", pkt.Data[0], seq)
	}
}

func TestNewConnectPacket(t *testing.T) {
	userId := uint64(1234567890)
	lan := "192.168.1.57:81"
	pkt := newConnectPacket(userId, lan)
	// check pkt
	if pkt.Magic != packetMagic {
		t.Errorf("pkt.Magic = %d, want %d", pkt.Magic, packetMagic)
	}
	if pkt.Type != PktTypeConnect {
		t.Errorf("pkt.Type = %d, want %d", pkt.Type, PktTypeConnect)
	}
	if pkt.Version != 0x01 {
		t.Errorf("pkt.Version = %d, want %d", pkt.Version, 0)
	}
	if pkt.Size != uint32(len(lan)+12) {
		t.Errorf("pkt.Size = %d, want %d", pkt.Size, len(lan)+12)
	}
	if binary.LittleEndian.Uint64(pkt.Data[:8]) != userId {
		t.Errorf("pkt.Data = %d, want %d", pkt.Data[:8], userId)
	}
	if binary.LittleEndian.Uint32(pkt.Data[8:12]) != uint32(len(lan)) {
		t.Errorf("pkt.Data = %d, want %d", pkt.Data[8:12], len(lan))
	}
	if string(pkt.Data[12:]) != lan {
		t.Errorf("pkt.Data = %s, want %s", pkt.Data[12:], lan)
	}
}

func TestEncode(t *testing.T) {
	userId := uint64(1234567890)
	lan := "192.168.1.57:81"
	pkt := newConnectPacket(userId, lan)
	buf := Encode(pkt)
	// check buf
	if len(buf) != packetHeaderSize+int(pkt.Size) {
		t.Errorf("len(buf) = %d, want %d", len(buf), packetHeaderSize+int(pkt.Size))
	}
	if binary.LittleEndian.Uint16(buf[:2]) != packetMagic {
		t.Errorf("buf[:2] = %d, want %d", binary.LittleEndian.Uint16(buf[:2]), packetMagic)
	}
	if buf[2] != PktTypeConnect {
		t.Errorf("buf[2] = %d, want %d", buf[2], PktTypeConnect)
	}
	if buf[3] != 0x01 {
		t.Errorf("buf[3] = %d, want %d", buf[3], 0x01)
	}
	if binary.LittleEndian.Uint32(buf[4:8]) != pkt.Size {
		t.Errorf("buf[4:8] = %d, want %d", binary.LittleEndian.Uint32(buf[4:8]), pkt.Size)
	}
	if binary.LittleEndian.Uint64(buf[8:16]) != userId {
		t.Errorf("buf[8:16] = %d, want %d", binary.LittleEndian.Uint64(buf[8:16]), userId)
	}
	if binary.LittleEndian.Uint32(buf[16:20]) != uint32(len(lan)) {
		t.Errorf("buf[16:20] = %d, want %d", binary.LittleEndian.Uint32(buf[16:20]), len(lan))
	}
	if string(buf[20:]) != lan {
		t.Errorf("buf[20:] = %s, want %s", buf[20:], lan)
	}
}
