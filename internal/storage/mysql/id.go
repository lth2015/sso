// Package mysql implements the first business persistence slice, independently of Hydra.
package mysql

import (
	"crypto/rand"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"time"
)

type ID [16]byte

// NewID generates UUIDv7 with CSPRNG randomness; IDs never confer authorization.
func NewID() (ID, error) {
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		return ID{}, err
	}
	ms := uint64(time.Now().UnixMilli())
	for i := 5; i >= 0; i-- {
		id[i] = byte(ms)
		ms >>= 8
	}
	id[6] = (id[6] & 0x0f) | 0x70
	id[8] = (id[8] & 0x3f) | 0x80
	return id, nil
}
func (id ID) String() string               { return hex.EncodeToString(id[:]) }
func (id ID) Value() (driver.Value, error) { return id[:], nil }
func (id *ID) Scan(src any) error {
	b, ok := src.([]byte)
	if !ok || len(b) != 16 {
		return errors.New("invalid binary ID")
	}
	copy(id[:], b)
	return nil
}
func (id ID) empty() bool { return id == ID{} }

func ParseID(value string) (ID, error) {
	var id ID
	b, err := hex.DecodeString(value)
	if err != nil || len(b) != 16 {
		return id, errors.New("invalid ID")
	}
	copy(id[:], b)
	return id, nil
}
