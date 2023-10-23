package util

import (
	"crypto/rand"
	"math/big"
)

// return a random id string composed of letters and numbers with length 12
func ContextId() string {
	// 定义可选字符集合
	charSet := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	charSetLength := big.NewInt(int64(len(charSet)))

	// 生成随机字符序列
	randomId := make([]byte, 12)
	for i := range randomId {
		randomIndex, _ := rand.Int(rand.Reader, charSetLength)
		randomId[i] = charSet[randomIndex.Int64()]
	}

	return string(randomId)
}
