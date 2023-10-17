package crox

import (
	"encoding/binary"
	"errors"
	"github.com/panjf2000/gnet/v2"
)

var ErrIncompletePacket = errors.New("incomplete packet")
var ErrInvalidMagicNumber = errors.New("invalid magic number")

const PktTypeAuth = 0x01
const PktTypeConnect = 0x02
const PktTypeData = 0x03
const PktTypeHeartbeat = 0x04
const PktTypeDisconnect = 0x05

const packetHeaderSize = 8
const packetMagic = 0x1314
const magicNumberSize = 2

type packet struct {
	Magic   uint16
	Type    uint8
	Version uint8
	Size    uint32
	Data    []byte
}

func Decode(c gnet.Conn) (*packet, error) {
	buf, _ := c.Peek(packetHeaderSize)
	if len(buf) < packetHeaderSize {
		return nil, ErrIncompletePacket
	}
	magicNumber := binary.LittleEndian.Uint16(buf[:magicNumberSize])
	if magicNumber != packetMagic {
		return nil, ErrInvalidMagicNumber
	}
	pkt := &packet{
		Magic:   magicNumber,
		Type:    buf[2],
		Version: buf[3],
		Size:    binary.LittleEndian.Uint32(buf[4:]),
	}
	msgLen := packetHeaderSize + int(pkt.Size)
	if c.InboundBuffered() < msgLen {
		return nil, ErrIncompletePacket
	}

	buf, _ = c.Peek(msgLen)
	data := buf[packetHeaderSize:msgLen]
	temp := make([]byte, pkt.Size)
	copy(temp, data)
	pkt.Data = temp
	c.Discard(msgLen)
	return pkt, nil
}

func NewDataPaket(data []byte) *packet {
	return NewPacket(PktTypeData, data)
}

func NewHeartbeatPacket(seq uint64) *packet {
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, seq)
	return NewPacket(PktTypeHeartbeat, data)
}

func NewConnectPacket(userId uint64, lan string) *packet {
	lanLen := len([]byte(lan))
	dataLen := 8 + 4 + lanLen
	data := make([]byte, dataLen)
	binary.LittleEndian.PutUint64(data, userId)
	binary.LittleEndian.PutUint32(data[8:], uint32(lanLen))
	copy(data[12:], lan)
	return NewPacket(PktTypeConnect, data)
}

func NewDisconnectPacket(userId uint64) *packet {
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, userId)
	return NewPacket(PktTypeDisconnect, data)
}

func NewPacket(pktType uint8, data []byte) *packet {
	var size uint32
	if data == nil {
		size = 0
	} else {
		size = uint32(len(data))
	}
	return &packet{
		Magic:   packetMagic,
		Type:    pktType,
		Version: 0x01,
		Size:    size,
		Data:    data,
	}
}

func Encode(pkt *packet) []byte {
	msgLen := packetHeaderSize + int(pkt.Size)
	data := make([]byte, msgLen)
	// write magic 0x1314
	binary.LittleEndian.PutUint16(data, pkt.Magic)
	// write type
	data[2] = pkt.Type
	// write version
	data[3] = pkt.Version
	// write body len
	binary.LittleEndian.PutUint32(data[4:], pkt.Size)
	// write body
	if pkt.Size > 0 {
		copy(data[packetHeaderSize:], pkt.Data)
	}
	return data
}
