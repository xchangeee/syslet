package api_test

import (
	"context"
	"testing"

	pb "codeberg.org/xchangeee/syslet/proto"
	"google.golang.org/grpc/metadata"

	"codeberg.org/xchangeee/syslet/internal/server/testutil"
)

// =============================================================================
// Category F: gRPC API
// =============================================================================

func TestAPI_F1_Apply(t *testing.T) {
	t.Run("single spec", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		spec := testutil.ContainerSpec("webapp").Image("nginx:latest").Build()

		resp, err := server.Apply(h.Context(), &pb.ApplyRequest{
			Specs: []string{spec},
		})

		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		// Response should be empty (specs are stored but not yet reconciled)
		_ = resp

		// Verify spec was stored
		specs, err := h.Store.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(specs) != 1 {
			t.Errorf("expected 1 spec stored, got %d", len(specs))
		}
	})

	t.Run("multiple specs", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		spec1 := testutil.ContainerSpec("app1").Image("nginx:latest").Build()
		spec2 := testutil.VolumeSpec("data").Label("app=test").Build()
		spec3 := testutil.NetworkSpec("internal").Subnet("10.89.0.0/24").Build()

		resp, err := server.Apply(h.Context(), &pb.ApplyRequest{
			Specs: []string{spec1, spec2, spec3},
		})

		if err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		_ = resp

		// Verify all specs stored
		specs, err := h.Store.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(specs) != 3 {
			t.Errorf("expected 3 specs stored, got %d", len(specs))
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		_, err := server.Apply(h.Context(), &pb.ApplyRequest{
			Specs: []string{"not valid json"},
		})

		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestAPI_F2_Status(t *testing.T) {
	t.Run("existing unit", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		// Setup: Apply and reconcile a container
		spec := testutil.ContainerSpec("webapp").Image("nginx:latest").Build()
		if err := h.ApplySpec(spec); err != nil {
			t.Fatalf("ApplySpec failed: %v", err)
		}
		h.Reconcile()

		// Query status
		resp, err := server.Status(h.Context(), &pb.StatusRequest{
			UnitName: "webapp.container",
		})

		if err != nil {
			t.Fatalf("Status failed: %v", err)
		}

		if len(resp.Units) != 1 {
			t.Fatalf("expected 1 unit, got %d", len(resp.Units))
		}

		unit := resp.Units[0]
		if unit.Name != "webapp.container" {
			t.Errorf("expected name webapp.container, got %s", unit.Name)
		}
		if unit.Type != pb.UnitType_UNIT_TYPE_CONTAINER {
			t.Errorf("expected container type, got %v", unit.Type)
		}
		if unit.DesiredState != pb.DesiredState_DESIRED_STATE_RUNNING {
			t.Errorf("expected running desired state, got %v", unit.DesiredState)
		}
		if unit.ActiveState != pb.ActiveState_ACTIVE_STATE_ACTIVE {
			t.Errorf("expected active state, got %v", unit.ActiveState)
		}
	})

	t.Run("non-existent unit returns inactive", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		// Querying status of a unit not in the store still works via D-Bus
		// It returns inactive state (the unit doesn't exist in systemd either)
		resp, err := server.Status(h.Context(), &pb.StatusRequest{
			UnitName: "nonexistent.container",
		})

		if err != nil {
			t.Fatalf("Status failed: %v", err)
		}

		if len(resp.Units) != 1 {
			t.Fatalf("expected 1 unit, got %d", len(resp.Units))
		}

		// Non-existent unit shows as inactive
		if resp.Units[0].ActiveState != pb.ActiveState_ACTIVE_STATE_INACTIVE {
			t.Errorf("expected inactive state, got %v", resp.Units[0].ActiveState)
		}
	})

	t.Run("unknown unit type errors", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		// Unit with unknown extension should error
		_, err := server.Status(h.Context(), &pb.StatusRequest{
			UnitName: "nonexistent.unknown",
		})

		if err == nil {
			t.Error("expected error for unknown unit type")
		}
	})

	t.Run("all units", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		// Setup: Apply multiple specs
		spec1 := testutil.ContainerSpec("app1").Image("nginx:latest").Build()
		spec2 := testutil.ContainerSpec("app2").Image("redis:7").Build()
		if err := h.ApplySpecs(spec1, spec2); err != nil {
			t.Fatalf("ApplySpecs failed: %v", err)
		}
		h.Reconcile()

		// Query all status (empty unit_name)
		resp, err := server.Status(h.Context(), &pb.StatusRequest{})

		if err != nil {
			t.Fatalf("Status failed: %v", err)
		}

		if len(resp.Units) != 2 {
			t.Errorf("expected 2 units, got %d", len(resp.Units))
		}
	})
}

func TestAPI_F3_List(t *testing.T) {
	t.Run("all units", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		// Setup
		spec1 := testutil.ContainerSpec("app1").Image("nginx:latest").Build()
		spec2 := testutil.VolumeSpec("data").Build()
		spec3 := testutil.NetworkSpec("internal").Build()
		if err := h.ApplySpecs(spec1, spec2, spec3); err != nil {
			t.Fatalf("ApplySpecs failed: %v", err)
		}
		h.Reconcile()

		// List all
		resp, err := server.List(h.Context(), &pb.ListRequest{})

		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(resp.Units) != 3 {
			t.Errorf("expected 3 units, got %d", len(resp.Units))
		}
	})

	t.Run("filter by type - container", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		// Setup
		spec1 := testutil.ContainerSpec("app1").Image("nginx:latest").Build()
		spec2 := testutil.VolumeSpec("data").Build()
		spec3 := testutil.NetworkSpec("internal").Build()
		if err := h.ApplySpecs(spec1, spec2, spec3); err != nil {
			t.Fatalf("ApplySpecs failed: %v", err)
		}
		h.Reconcile()

		// List containers only
		resp, err := server.List(h.Context(), &pb.ListRequest{
			TypeFilter: pb.UnitType_UNIT_TYPE_CONTAINER,
		})

		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(resp.Units) != 1 {
			t.Errorf("expected 1 container, got %d", len(resp.Units))
		}
		if resp.Units[0].Type != pb.UnitType_UNIT_TYPE_CONTAINER {
			t.Errorf("expected container type, got %v", resp.Units[0].Type)
		}
	})

	t.Run("filter by type - volume", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		// Setup
		spec1 := testutil.ContainerSpec("app1").Image("nginx:latest").Build()
		spec2 := testutil.VolumeSpec("data").Build()
		if err := h.ApplySpecs(spec1, spec2); err != nil {
			t.Fatalf("ApplySpecs failed: %v", err)
		}
		h.Reconcile()

		// List volumes only
		resp, err := server.List(h.Context(), &pb.ListRequest{
			TypeFilter: pb.UnitType_UNIT_TYPE_VOLUME,
		})

		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(resp.Units) != 1 {
			t.Errorf("expected 1 volume, got %d", len(resp.Units))
		}
	})

	t.Run("empty list", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		resp, err := server.List(h.Context(), &pb.ListRequest{})

		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(resp.Units) != 0 {
			t.Errorf("expected 0 units, got %d", len(resp.Units))
		}
	})
}

func TestAPI_F4_Delete(t *testing.T) {
	t.Run("delete existing container", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		// Setup: Create and reconcile a container
		spec := testutil.ContainerSpec("webapp").Image("nginx:latest").Build()
		if err := h.ApplySpec(spec); err != nil {
			t.Fatalf("ApplySpec failed: %v", err)
		}
		h.Reconcile()

		// Verify it exists
		h.AssertUnitFileExists("webapp.container")
		h.AssertUnitStarted("webapp.container")

		// Delete
		resp, err := server.Delete(h.Context(), &pb.DeleteRequest{
			UnitName: "webapp.container",
		})

		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		if resp.Message == "" {
			t.Error("expected delete message")
		}

		// Verify stopped and removed
		h.AssertUnitStopped("webapp.container")
		h.AssertUnitFileNotExists("webapp.container")

		// Verify removed from store
		specs, err := h.Store.List()
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(specs) != 0 {
			t.Errorf("expected 0 specs after delete, got %d", len(specs))
		}
	})

	t.Run("delete by bare name", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		// Setup
		spec := testutil.ContainerSpec("webapp").Image("nginx:latest").Build()
		if err := h.ApplySpec(spec); err != nil {
			t.Fatalf("ApplySpec failed: %v", err)
		}
		h.Reconcile()

		// Delete using bare name (no extension)
		resp, err := server.Delete(h.Context(), &pb.DeleteRequest{
			UnitName: "webapp",
		})

		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		_ = resp

		// Verify removed
		h.AssertUnitFileNotExists("webapp.container")
	})

	t.Run("delete volume", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		// Setup
		spec := testutil.VolumeSpec("data").Build()
		if err := h.ApplySpec(spec); err != nil {
			t.Fatalf("ApplySpec failed: %v", err)
		}
		h.Reconcile()

		// Delete
		resp, err := server.Delete(h.Context(), &pb.DeleteRequest{
			UnitName: "data.volume",
		})

		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		_ = resp

		// Volumes are not stopped (not startable), but file should be removed
		h.AssertUnitFileNotExists("data.volume")
	})

	t.Run("delete non-existent unit", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		_, err := server.Delete(h.Context(), &pb.DeleteRequest{
			UnitName: "nonexistent",
		})

		if err == nil {
			t.Error("expected error for non-existent unit")
		}
	})

	t.Run("delete with empty name", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		_, err := server.Delete(h.Context(), &pb.DeleteRequest{
			UnitName: "",
		})

		if err == nil {
			t.Error("expected error for empty unit name")
		}
	})
}

func TestAPI_F5_Logs(t *testing.T) {
	// Note: Logs tests are limited because journal.Read/Follow call journalctl subprocess
	// which won't work in unit tests. We test the error cases here.

	t.Run("empty unit name", func(t *testing.T) {
		h := testutil.NewHarness(t)
		server := h.APIServer()

		// Create a mock stream
		stream := &mockLogsStream{ctx: context.Background()}

		err := server.Logs(&pb.LogsRequest{
			UnitName: "",
		}, stream)

		if err == nil {
			t.Error("expected error for empty unit name")
		}
	})
}

// mockLogsStream implements pb.SysletService_LogsServer for testing
type mockLogsStream struct {
	ctx     context.Context
	entries []*pb.LogEntry
}

func (m *mockLogsStream) Send(entry *pb.LogEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockLogsStream) Context() context.Context {
	return m.ctx
}

func (m *mockLogsStream) SetHeader(metadata.MD) error  { return nil }
func (m *mockLogsStream) SendHeader(metadata.MD) error { return nil }
func (m *mockLogsStream) SetTrailer(metadata.MD)       {}
func (m *mockLogsStream) SendMsg(msg any) error        { return nil }
func (m *mockLogsStream) RecvMsg(msg any) error        { return nil }
