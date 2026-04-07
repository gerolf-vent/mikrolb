package utils

import (
	"crypto/sha256"
	"strings"
)

func GenerateUniqueHash(count int, parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "#")))
	if count > len(hash) {
		count = len(hash)
	}
	const charset = "0123456789abcdefghijklmnopqrstuvwxyz"
	var result string
	for i := 0; i < count; i++ {
		result += string(charset[hash[i]%byte(len(charset))])
	}
	return result
}
