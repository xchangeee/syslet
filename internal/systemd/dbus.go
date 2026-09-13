package systemd

import (
	"context"

	"github.com/coreos/go-systemd/v22/dbus"
)

// DBusConn abstracts the systemd D-Bus connection for dependency injection.
type DBusConn interface {
	Close()
	ReloadContext(ctx context.Context) error
	GetUnitPropertiesContext(ctx context.Context, unit string) (map[string]any, error)
	StartUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error)
	StopUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error)
	ReloadUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error)
}

// NewDBusConnection creates a real D-Bus connection to systemd.
func NewDBusConnection(ctx context.Context) (DBusConn, error) {
	return dbus.NewSystemConnectionContext(ctx)
}
