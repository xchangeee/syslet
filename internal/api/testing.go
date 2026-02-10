package api

import (
	"context"
)

// mockDBusConn is a minimal test helper that implements the DBusConn interface
// for basic API spec tests. It provides no-op implementations for all methods.
//
// This mock is intentionally simple and stateless, suitable for tests that only
// need a DBusConn to construct a systemd.Client but don't actually interact with systemd.
// For tests requiring more sophisticated mocking behavior, use systemd.MockDBusConn instead.
type mockDBusConn struct{}

func (m *mockDBusConn) Close() {}

func (m *mockDBusConn) ReloadContext(ctx context.Context) error {
	return nil
}

func (m *mockDBusConn) GetUnitPropertiesContext(ctx context.Context, unit string) (map[string]interface{}, error) {
	return nil, nil
}

func (m *mockDBusConn) StartUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error) {
	return 0, nil
}

func (m *mockDBusConn) StopUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error) {
	return 0, nil
}
