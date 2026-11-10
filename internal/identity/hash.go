package identity

import (
	"crypto/sha256"
	"encoding/hex"
)

func hashAlias(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
