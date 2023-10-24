package util

import (
	"crypto/rand"
	"encoding/binary"
	"hash/crc32"
	"math/big"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

var (
	processId        = os.Getpid()
	nextSeq   uint32 = 0
	machineId        = getMacAddress()
)

const (
	processIdLen = 4
	seqLen       = 4
	timestampLen = 8
	randomLen    = 4
)

func getMacAddress() []byte {
	interfaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}
	for _, inter := range interfaces {
		// 过滤掉回环接口和虚拟接口
		if inter.Flags&net.FlagLoopback != 0 || inter.Flags&net.FlagUp == 0 {
			continue
		}

		// 获取MAC地址
		mac := inter.HardwareAddr
		return mac
	}
	return nil
}

// ContextId return a random id string composed of letters and numbers with length 12
func ContextId() string {
	data := make([]byte, len(machineId)+processIdLen+seqLen+timestampLen+randomLen)
	i := 0
	copy(data[i:], machineId)
	i += len(machineId)
	binary.LittleEndian.PutUint32(data[i:], uint32(processId))
	i += processIdLen
	seq := atomic.AddUint32(&nextSeq, 1)
	binary.LittleEndian.PutUint32(data[i:], seq)
	i += seqLen
	nanoTime := time.Now().UnixNano()
	milliTime := time.Now().UnixMilli()
	ts := reverseBits(nanoTime) ^ milliTime
	binary.LittleEndian.PutUint64(data[i:], uint64(ts))
	i += timestampLen
	randNum, _ := rand.Int(rand.Reader, big.NewInt(1<<32-1))
	binary.LittleEndian.PutUint32(data[i:], uint32(randNum.Int64()))
	i += randomLen
	if i != len(data) {
		panic("invalid id length")
	}
	hashCode := crc32.ChecksumIEEE(data)
	// 将hash转化为16进制字符串
	return strconv.FormatInt(int64(hashCode), 16)
}

func reverseBits(i int64) int64 {
	i = (i&0x5555555555555555)<<1 | (i>>1)&0x5555555555555555
	i = (i&0x3333333333333333)<<2 | (i>>2)&0x3333333333333333
	i = (i&0x0f0f0f0f0f0f0f0f)<<4 | (i>>4)&0x0f0f0f0f0f0f0f0f
	i = (i&0x00ff00ff00ff00ff)<<8 | (i>>8)&0x00ff00ff00ff00ff
	i = (i << 48) | ((i & 0xffff0000) << 16) |
		((i >> 16) & 0xffff0000) | (i >> 48)
	return i
}
