package identity

import (
	"errors"
	"os"
)

// DeliveryFailureCode exposes only controlled categories, never paths or payloads.
func DeliveryFailureCode(err error) string {
	if errors.Is(err, os.ErrPermission) {
		return "mailbox_permission"
	}
	var path *os.PathError
	if errors.As(err, &path) {
		switch path.Op {
		case "open", "sync", "link", "lstat", "write":
			return "filesystem_" + path.Op
		default:
			return "filesystem_error"
		}
	}
	switch err.Error() {
	case "invalid notification", "notification metadata mismatch":
		return "notification_authentication"
	case "mailbox must be a private directory":
		return "mailbox_not_private"
	case "invalid mailbox entry", "mailbox entry conflict":
		return "mailbox_conflict"
	}
	return "authority_or_delivery_error"
}
