package identity

import (
	"bytes"
	"crypto/cipher"
	"errors"
	"os"
	"path/filepath"

	store "github.com/lth2015/sso/internal/storage/mysql"
)

// FileSink is a private local test mailbox. It has no SMTP/network delivery.
func FileSink(dir string, aead cipher.AEAD) func(store.Notification) error {
	return func(n store.Notification) error {
		plain, err := Decrypt(aead, n)
		if err != nil {
			return err
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return errors.New("mailbox must be a private directory")
		}
		target := filepath.Join(dir, n.ID.String()+".json")
		temp, err := os.CreateTemp(dir, ".delivery-")
		if err != nil {
			return err
		}
		name := temp.Name()
		defer os.Remove(name)
		if _, err = temp.Write(plain); err != nil {
			temp.Close()
			return err
		}
		if err = temp.Sync(); err != nil {
			temp.Close()
			return err
		}
		if err = temp.Close(); err != nil {
			return err
		}
		// Link publishes the complete private file atomically without overwriting.
		if err = os.Link(name, target); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err := os.Lstat(target)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
				return errors.New("invalid mailbox entry")
			}
			previous, err := os.ReadFile(target)
			if err != nil {
				return err
			}
			if !bytes.Equal(previous, plain) {
				return errors.New("mailbox entry conflict")
			}
		}
		directory, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer directory.Close()
		return directory.Sync()
	}
}
