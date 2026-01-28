// Package api implements the gRPC server for syslet.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	pb "codeberg.org/xchangeee/syslet/proto"

	"codeberg.org/xchangeee/syslet/internal/containerconfig"
	"codeberg.org/xchangeee/syslet/internal/daemon"
	"codeberg.org/xchangeee/syslet/internal/journal"
	"codeberg.org/xchangeee/syslet/internal/parser"
	"codeberg.org/xchangeee/syslet/internal/spec"
	"codeberg.org/xchangeee/syslet/internal/systemd"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the SysletService gRPC service.
type Server struct {
	pb.UnimplementedSysletServiceServer

	daemon  *daemon.Daemon
	systemd *systemd.Client
	config  *containerconfig.Manager
	logger  *slog.Logger
}

// NewServer creates a new gRPC server.
func NewServer(d *daemon.Daemon, sd *systemd.Client, cfg *containerconfig.Manager, logger *slog.Logger) *Server {
	return &Server{
		daemon:  d,
		systemd: sd,
		config:  cfg,
		logger:  logger,
	}
}

func (s *Server) Apply(ctx context.Context, req *pb.ApplyRequest) (*pb.ApplyResponse, error) {
	if err := s.daemon.ApplySpecs(ctx, req.Specs); err != nil {
		return nil, status.Errorf(codes.Internal, "apply specs: %v", err)
	}

	return &pb.ApplyResponse{}, nil
}

func (s *Server) Status(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	resp := &pb.StatusResponse{}

	if req.UnitName != "" {
		us, err := s.getUnitStatus(ctx, req.UnitName)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "unit %s: %v", req.UnitName, err)
		}
		resp.Units = append(resp.Units, us)
		return resp, nil
	}

	// All units
	specs, err := s.loadAllSpecs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "loading specs: %v", err)
	}
	for _, cs := range specs {
		unitName := cs.Name + "." + cs.Type
		us, err := s.getUnitStatus(ctx, unitName)
		if err != nil {
			s.logger.Error("status error", "unit", unitName, "error", err)
			continue
		}
		resp.Units = append(resp.Units, us)
	}

	return resp, nil
}

func (s *Server) List(ctx context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
	specs, err := s.loadAllSpecs()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "loading specs: %v", err)
	}

	resp := &pb.ListResponse{}
	for _, cs := range specs {
		unitName := cs.Name + "." + cs.Type
		unitType := parser.UnitTypeFromExtension(unitName)
		if req.TypeFilter != pb.UnitType_UNIT_TYPE_UNSPECIFIED && pbUnitType(unitType) != req.TypeFilter {
			continue
		}
		us, err := s.getUnitStatus(ctx, unitName)
		if err != nil {
			s.logger.Error("status error", "unit", unitName, "error", err)
			continue
		}
		resp.Units = append(resp.Units, us)
	}

	return resp, nil
}

func (s *Server) Logs(req *pb.LogsRequest, stream pb.SysletService_LogsServer) error {
	if req.UnitName == "" {
		return status.Error(codes.InvalidArgument, "unit_name is required")
	}

	ctx := stream.Context()

	if !req.Follow {
		entries, err := journal.Read(ctx, req.UnitName, int(req.Lines))
		if err != nil {
			return status.Errorf(codes.Internal, "reading logs: %v", err)
		}
		for _, e := range entries {
			if err := stream.Send(&pb.LogEntry{
				Timestamp: e.Timestamp,
				Message:   e.Message,
				Priority:  e.Priority,
			}); err != nil {
				return err
			}
		}
		return nil
	}

	ch := make(chan journal.Entry, 100)
	go func() {
		_ = journal.Follow(ctx, req.UnitName, int(req.Lines), ch)
	}()

	for entry := range ch {
		if err := stream.Send(&pb.LogEntry{
			Timestamp: entry.Timestamp,
			Message:   entry.Message,
			Priority:  entry.Priority,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	if req.UnitName == "" {
		return nil, status.Error(codes.InvalidArgument, "unit_name is required")
	}

	// unit_name can be "webapp.container" or just "webapp" — try to resolve
	unitName := req.UnitName
	unitType := parser.UnitTypeFromExtension(unitName)

	if unitType == parser.UnitTypeUnknown {
		// Try to find it in the store by searching all types
		for _, t := range []string{"container", "volume", "network"} {
			_, err := s.daemon.Store().Get(unitName, t)
			if err == nil {
				unitType = parser.UnitTypeFromExtension(unitName + "." + t)
				unitName = unitName + "." + t
				// Delete from store
				if err := s.daemon.Store().Delete(req.UnitName, t); err != nil {
					s.logger.Warn("store delete failed", "name", req.UnitName, "error", err)
				}
				break
			}
		}
		if unitType == parser.UnitTypeUnknown {
			return nil, status.Errorf(codes.NotFound, "unit %q not found", req.UnitName)
		}
	} else {
		baseName := parser.UnitBaseName(unitName)
		typeName := unitType.String()
		if err := s.daemon.Store().Delete(baseName, typeName); err != nil {
			s.logger.Warn("store delete failed", "name", baseName, "error", err)
		}
	}

	// Stop the unit first
	if unitType.IsStartable() {
		if err := s.systemd.StopUnit(ctx, unitName, unitType); err != nil {
			s.logger.Warn("stop failed during delete", "unit", unitName, "error", err)
		}
	}

	// Remove unit file from systemd
	if err := s.systemd.RemoveUnitFile(unitName, unitType); err != nil {
		return nil, status.Errorf(codes.Internal, "removing unit file: %v", err)
	}

	// Remove config files
	s.config.RemoveAll(unitName)

	// Daemon reload
	if err := s.systemd.DaemonReload(ctx); err != nil {
		s.logger.Warn("daemon-reload failed during delete", "error", err)
	}

	return &pb.DeleteResponse{
		Message: fmt.Sprintf("deleted %s", req.UnitName),
	}, nil
}

func (s *Server) loadAllSpecs() ([]spec.ContainerSpec, error) {
	jsons, err := s.daemon.Store().List()
	if err != nil {
		return nil, err
	}
	var specs []spec.ContainerSpec
	for _, j := range jsons {
		var cs spec.ContainerSpec
		if err := json.Unmarshal([]byte(j), &cs); err != nil {
			return nil, err
		}
		specs = append(specs, cs)
	}
	return specs, nil
}

func (s *Server) getUnitStatus(ctx context.Context, unitName string) (*pb.UnitStatus, error) {
	unitType := parser.UnitTypeFromExtension(unitName)
	if unitType == parser.UnitTypeUnknown {
		return nil, fmt.Errorf("unknown unit type")
	}

	state, err := s.systemd.GetUnitState(ctx, unitName, unitType)
	if err != nil {
		return nil, err
	}

	// Look up desired state from store
	baseName := parser.UnitBaseName(unitName)
	var desiredState pb.DesiredState
	specJSON, err := s.daemon.Store().Get(baseName, unitType.String())
	if err == nil {
		var cs spec.ContainerSpec
		if err := json.Unmarshal([]byte(specJSON), &cs); err == nil {
			switch cs.DesiredState {
			case "running":
				desiredState = pb.DesiredState_DESIRED_STATE_RUNNING
			case "stopped":
				desiredState = pb.DesiredState_DESIRED_STATE_STOPPED
			}
		}
	}

	cfgFiles, _ := s.config.ListFiles(unitName)

	us := &pb.UnitStatus{
		Name:         unitName,
		Type:         pbUnitType(unitType),
		DesiredState: desiredState,
		ActiveState:  pbActiveState(state.ActiveState),
		Enabled:      state.Enabled,
		ConfigFiles:  cfgFiles,
	}

	if rs, err := s.daemon.Store().GetReconcileStatus(baseName, unitType.String()); err == nil {
		if !rs.LastReconciled.IsZero() {
			us.LastReconciled = rs.LastReconciled.Format(time.RFC3339)
		}
		us.Error = rs.Error
	}

	return us, nil
}

func pbUnitType(t parser.UnitType) pb.UnitType {
	switch t {
	case parser.UnitTypeContainer:
		return pb.UnitType_UNIT_TYPE_CONTAINER
	case parser.UnitTypeVolume:
		return pb.UnitType_UNIT_TYPE_VOLUME
	case parser.UnitTypeNetwork:
		return pb.UnitType_UNIT_TYPE_NETWORK
	default:
		return pb.UnitType_UNIT_TYPE_UNSPECIFIED
	}
}

func pbActiveState(s string) pb.ActiveState {
	switch s {
	case "active":
		return pb.ActiveState_ACTIVE_STATE_ACTIVE
	case "inactive":
		return pb.ActiveState_ACTIVE_STATE_INACTIVE
	case "failed":
		return pb.ActiveState_ACTIVE_STATE_FAILED
	case "activating":
		return pb.ActiveState_ACTIVE_STATE_ACTIVATING
	case "deactivating":
		return pb.ActiveState_ACTIVE_STATE_DEACTIVATING
	default:
		return pb.ActiveState_ACTIVE_STATE_UNSPECIFIED
	}
}
