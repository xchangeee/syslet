package util

import (
	"crypto/sha256"
	"fmt"
)

func Sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}
