package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"fmt"
)

// AesECB 实现 ECB 模式的块加密/解密
type AesECB struct {
	block cipher.Block
}

func NewAesECB(key []byte) (*AesECB, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return &AesECB{block: block}, nil
}

// pkcs5Pad PKCS5 填充（实际是 PKCS7，块大小 16）
func pkcs5Pad(src []byte, blockSize int) []byte {
	padding := blockSize - len(src)%blockSize
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(src, padText...)
}

// pkcs5Unpad 去除 PKCS5 填充
func pkcs5Unpad(src []byte) ([]byte, error) {
	length := len(src)
	if length == 0 {
		return nil, errors.New("unpad: empty data")
	}
	unpadding := int(src[length-1])
	if unpadding > length {
		return nil, errors.New("unpad: invalid padding")
	}
	return src[:length-unpadding], nil
}

// Encrypt 加密数据（ECB 模式，自动填充）
func (e *AesECB) Encrypt(plaintext []byte) ([]byte, error) {
	blockSize := e.block.BlockSize()
	fmt.Println("Block size:", blockSize)
	// 填充
	padded := pkcs5Pad(plaintext, blockSize)
	if len(padded)%blockSize != 0 {
		return nil, errors.New("encrypt: padded data not a multiple of block size")
	}
	ciphertext := make([]byte, len(padded))
	// 分组加密
	for i := 0; i < len(padded); i += blockSize {
		e.block.Encrypt(ciphertext[i:i+blockSize], padded[i:i+blockSize])
	}
	return ciphertext, nil
}

// Decrypt 解密数据（ECB 模式，自动去除填充）
func (e *AesECB) Decrypt(ciphertext []byte) ([]byte, error) {
	blockSize := e.block.BlockSize()
	if len(ciphertext)%blockSize != 0 {
		return nil, errors.New("decrypt: ciphertext not a multiple of block size")
	}
	plaintext := make([]byte, len(ciphertext))
	// 分组解密
	for i := 0; i < len(ciphertext); i += blockSize {
		e.block.Decrypt(plaintext[i:i+blockSize], ciphertext[i:i+blockSize])
	}
	// 去除填充
	return pkcs5Unpad(plaintext)
}

// EncryptStr 加密字符串，返回十六进制字符串
func EncryptStr(plaintext, key string) (string, error) {
	aesECB, err := NewAesECB([]byte(key))
	if err != nil {
		return "", err
	}
	ciphertext, err := aesECB.Encrypt([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(ciphertext), nil
}

// DecryptStr 解密十六进制字符串，返回原始字符串
func DecryptStr(ciphertextHex, key string) (string, error) {
	ciphertext, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", err
	}
	aesECB, err := NewAesECB([]byte(key))
	if err != nil {
		return "", err
	}
	plaintext, err := aesECB.Decrypt(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func main() {
	key := "12345678901234561234567890123456"
	// plain := "Aa123456"
	plain := "Wjq367078@"

	// 加密
	encrypted, err := EncryptStr(plain, key)
	if err != nil {
		fmt.Println("加密失败:", err)
		return
	}
	fmt.Println("加密结果(hex):", encrypted)

	// 解密
	decrypted, err := DecryptStr(encrypted, key)
	if err != nil {
		fmt.Println("解密失败:", err)
		return
	}
	fmt.Println("解密结果:", decrypted)

	// 验证与 Java 输出一致（假设 Java 示例中的加密结果已知）
	// 注意：Java 示例中加密 "Aa123456" 得到 "081b1ac0f00ad626916a6924ca9ad574"
	// 但实际可能因填充和密钥不同而有差异，此处仅为演示
	testHex := "081b1ac0f00ad626916a6924ca9ad574"
	decrypted2, err := DecryptStr(testHex, key)
	if err != nil {
		fmt.Println("解密测试串失败:", err)
	} else {
		fmt.Printf("解密测试串 %s 得到: %s\n", testHex, decrypted2)
	}

	// 检查是否是 AES 加密字符串（类似 Java 的 isAESEncryptedString）
	check := func(s string) bool {
		// 简单检查：长度是16的倍数且能解密成功
		if len(s) == 0 || len(s)%32 != 0 { // 十六进制字符串长度应为16的倍数 *2 = 32的倍数
			return false
		}
		_, err := DecryptStr(s, key)
		return err == nil
	}
	fmt.Println("testHex 是否有效:", check(testHex))
	fmt.Println("无效串是否有效:", check("1234"))
}
