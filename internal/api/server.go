// Package api implements the gRPC server for rsystemd.
package api

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	pb "codeberg.org/xchangeee/rsystemd/proto"

	"codeberg.org/xchangeee/rsystemd/internal/config"
	"codeberg.org/xchangeee/rsystemd/internal/daemon"
	"codeberg.org/xchangeee/rsystemd/internal/journal"
	"codeberg.org/xchangeee/rsystemd/internal/parser"
	"codeberg.org/xchangeee/rsystemd/internal/systemd"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the RsystemdService gRPC service.
type Server struct {
	pb.UnimplementedRsystemdServiceServer

	daemon  *daemon.Daemon
	systemd *systemd.Client
	config  *config.Manager
	logger  *slog.Logger
}

// NewServer creates a new gRPC server.
func NewServer(d *daemon.Daemon, sd *systemd.Client, cfg *config.Manager, logger *slog.Logger) *Server {
	return &Server{
		daemon:  d,
		systemd: sd,
		config:  cfg,
		logger:  logger,
	}
}

func (s *Server) Apply(ctx context.Context, req *pb.ApplyRequest) (*pb.ApplyResponse, error) {
	// Convert proto units to raw content map
	rawUnits := make(map[string]string)
	for _, u := range req.Units {
		rawUnits[u.Name] = u.Content
	}

	// Convert proto configs
	var cfgFiles []config.ConfigFile
	for _, c := range req.Configs {
		cfgFiles = append(cfgFiles, config.ConfigFile{
			UnitName: c.UnitName,
			Filename: c.Filename,
			Content:  c.Content,
		})
	}

	results := s.daemon.ApplyUnitsAndConfigs(ctx, rawUnits, cfgFiles)

	resp := &pb.ApplyResponse{}
	for _, r := range results {
		resp.Results = append(resp.Results, &pb.UnitResult{
			Name:    r.Name,
			Changed: r.Changed,
			Message: r.Message,
		})
	}

	return resp, nil
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
	units, err := daemon.LoadUnitsFromDir(daemon.DefaultUnitsDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "loading units: %v", err)
	}
	for _, u := range units {
		us, err := s.getUnitStatus(ctx, u.Name)
		if err != nil {
			s.logger.Error("status error", "unit", u.Name, "error", err)
			continue
		}
		resp.Units = append(resp.Units, us)
	}

	return resp, nil
}

func (s *Server) List(ctx context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
	units, err := daemon.LoadUnitsFromDir(daemon.DefaultUnitsDir)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "loading units: %v", err)
	}

	resp := &pb.ListResponse{}
	for _, u := range units {
		if req.TypeFilter != pb.UnitType_UNIT_TYPE_UNSPECIFIED && pbUnitType(u.Type) != req.TypeFilter {
			continue
		}
		us, err := s.getUnitStatus(ctx, u.Name)
		if err != nil {
			s.logger.Error("status error", "unit", u.Name, "error", err)
			continue
		}
		resp.Units = append(resp.Units, us)
	}

	return resp, nil
}

func (s *Server) Logs(req *pb.LogsRequest, stream pb.RsystemdService_LogsServer) error {
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

	unitType := parser.UnitTypeFromExtension(req.UnitName)
	if unitType == parser.UnitTypeUnknown {
		return nil, status.Errorf(codes.InvalidArgument, "unknown unit type: %s", req.UnitName)
	}

	// Stop the unit first
	if unitType.IsStartable() {
		if err := s.systemd.StopUnit(ctx, req.UnitName, unitType); err != nil {
			s.logger.Warn("stop failed during delete", "unit", req.UnitName, "error", err)
		}
	}

	// Remove unit file from systemd
	if err := s.systemd.RemoveUnitFile(req.UnitName, unitType); err != nil {
		return nil, status.Errorf(codes.Internal, "removing unit file: %v", err)
	}

	// Remove config files
	s.config.RemoveAll(req.UnitName, unitType)

	// Daemon reload
	if err := s.systemd.DaemonReload(ctx); err != nil {
		s.logger.Warn("daemon-reload failed during delete", "error", err)
	}

	return &pb.DeleteResponse{
		Message: fmt.Sprintf("deleted %s", req.UnitName),
	}, nil
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

	// Parse the managed unit file for desired state
	units, err := daemon.LoadUnitsFromDir(daemon.DefaultUnitsDir)
	if err != nil {
		return nil, err
	}
	var desiredState pb.DesiredState
	for _, u := range units {
		if u.Name == unitName {
			desiredState = pbDesiredState(u.DesiredState)
			break
		}
	}

	cfgFiles, _ := s.config.ListFiles(unitName, unitType)

	return &pb.UnitStatus{
		Name:           unitName,
		Type:           pbUnitType(unitType),
		DesiredState:   desiredState,
		ActiveState:    pbActiveState(state.ActiveState),
		Enabled:        state.Enabled,
		ConfigFiles:    cfgFiles,
		LastReconciled: time.Now().Format(time.RFC3339),
	}, nil
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

func pbDesiredState(s parser.DesiredState) pb.DesiredState {
	switch s {
	case parser.DesiredStateRunning:
		return pb.DesiredState_DESIRED_STATE_RUNNING
	case parser.DesiredStateStopped:
		return pb.DesiredState_DESIRED_STATE_STOPPED
	default:
		return pb.DesiredState_DESIRED_STATE_UNSPECIFIED
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
