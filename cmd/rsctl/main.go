package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	pb "codeberg.org/xchangeee/syslet/proto"

	"codeberg.org/xchangeee/syslet/internal/ctlconfig"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	serverAddr  string
	contextName string
)

func main() {
	root := &cobra.Command{
		Use:   "rsctl",
		Short: "syslet CLI - manage container units declaratively",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// context subcommand manages its own config; skip resolution
			if cmd.Name() == "context" || (cmd.Parent() != nil && cmd.Parent().Name() == "context") {
				return nil
			}
			return resolveServerAddr(cmd)
		},
	}

	root.PersistentFlags().StringVar(&contextName, "context", "", "named context to use")

	root.AddCommand(applyCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(listCmd())
	root.AddCommand(logsCmd())
	root.AddCommand(deleteCmd())
	root.AddCommand(contextCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func resolveServerAddr(cmd *cobra.Command) error {
	cfg, err := ctlconfig.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// --context flag
	if contextName != "" {
		addr, err := cfg.ServerAddrForContext(contextName)
		if err != nil {
			return err
		}
		serverAddr = addr
		return nil
	}

	// current-context from config
	if addr := cfg.ServerAddr(); addr != "" {
		serverAddr = addr
		return nil
	}

	// fallback
	serverAddr = "localhost:7233"
	return nil
}

func contextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Manage server contexts",
	}
	cmd.AddCommand(contextListCmd())
	cmd.AddCommand(contextUseCmd())
	cmd.AddCommand(contextAddCmd())
	cmd.AddCommand(contextRemoveCmd())
	return cmd
}

func contextListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all contexts",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := ctlconfig.Load()
			if err != nil {
				return err
			}
			if len(cfg.Contexts) == 0 {
				fmt.Println("No contexts configured. Use 'rsctl context add' to add one.")
				return nil
			}
			for name, ctx := range cfg.Contexts {
				marker := "  "
				if name == cfg.CurrentContext {
					marker = "* "
				}
				fmt.Printf("%s%-20s %s\n", marker, name, ctx.Server)
			}
			return nil
		},
	}
}

func contextUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := ctlconfig.Load()
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := cfg.Contexts[name]; !ok {
				return fmt.Errorf("context %q not found", name)
			}
			cfg.CurrentContext = name
			if err := ctlconfig.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("Switched to context %q (%s)\n", name, cfg.Contexts[name].Server)
			return nil
		},
	}
}

func contextAddCmd() *cobra.Command {
	var server string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a new context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if server == "" {
				return fmt.Errorf("--server is required")
			}
			cfg, err := ctlconfig.Load()
			if err != nil {
				return err
			}
			name := args[0]
			cfg.Contexts[name] = ctlconfig.Context{Server: server}
			// Set as current if it's the first context
			if len(cfg.Contexts) == 1 {
				cfg.CurrentContext = name
			}
			if err := ctlconfig.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("Added context %q (%s)\n", name, server)
			return nil
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "server address (host:port)")
	return cmd
}

func contextRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := ctlconfig.Load()
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := cfg.Contexts[name]; !ok {
				return fmt.Errorf("context %q not found", name)
			}
			delete(cfg.Contexts, name)
			if cfg.CurrentContext == name {
				cfg.CurrentContext = ""
			}
			if err := ctlconfig.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("Removed context %q\n", name)
			return nil
		},
	}
}

func connect() (pb.SysletServiceClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to %s: %w", serverAddr, err)
	}
	return pb.NewSysletServiceClient(conn), conn, nil
}

func applyCmd() *cobra.Command {
	var filePath string

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply JSON spec files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if filePath == "" {
				return fmt.Errorf("--file/-f is required")
			}

			specs, err := loadSpecs(filePath)
			if err != nil {
				return err
			}

			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.Apply(context.Background(), &pb.ApplyRequest{
				Specs: specs,
			})
			if err != nil {
				return fmt.Errorf("apply: %w", err)
			}

			for _, r := range resp.Results {
				status := "unchanged"
				if r.Changed {
					status = "changed"
				}
				fmt.Printf("%-30s %s  %s\n", r.Name, status, r.Message)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&filePath, "file", "f", "", "JSON spec file or directory")
	return cmd
}

func loadSpecs(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return []string{string(data)}, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var specs []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(path, e.Name()))
		if err != nil {
			return nil, err
		}
		specs = append(specs, string(data))
	}
	return specs, nil
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [unit]",
		Short: "Show status of managed units",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			unitName := ""
			if len(args) > 0 {
				unitName = args[0]
			}

			resp, err := client.Status(context.Background(), &pb.StatusRequest{UnitName: unitName})
			if err != nil {
				return fmt.Errorf("status: %w", err)
			}

			for _, u := range resp.Units {
				printUnitStatus(u)
			}
			return nil
		},
	}
}

func listCmd() *cobra.Command {
	var typeFilter string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List managed units",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			req := &pb.ListRequest{}
			if typeFilter != "" {
				req.TypeFilter = parseTypeFilter(typeFilter)
			}

			resp, err := client.List(context.Background(), req)
			if err != nil {
				return fmt.Errorf("list: %w", err)
			}

			fmt.Printf("%-30s %-12s %-12s %-10s %-8s\n", "NAME", "TYPE", "DESIRED", "ACTIVE", "ENABLED")
			for _, u := range resp.Units {
				fmt.Printf("%-30s %-12s %-12s %-10s %-8v\n",
					u.Name,
					strings.TrimPrefix(u.Type.String(), "UNIT_TYPE_"),
					strings.TrimPrefix(u.DesiredState.String(), "DESIRED_STATE_"),
					strings.TrimPrefix(u.ActiveState.String(), "ACTIVE_STATE_"),
					u.Enabled,
				)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&typeFilter, "type", "", "filter by type (container, volume, network)")
	return cmd
}

func logsCmd() *cobra.Command {
	var follow bool
	var lines int32

	cmd := &cobra.Command{
		Use:   "logs <unit>",
		Short: "Stream journal logs for a unit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			stream, err := client.Logs(context.Background(), &pb.LogsRequest{
				UnitName: args[0],
				Follow:   follow,
				Lines:    lines,
			})
			if err != nil {
				return fmt.Errorf("logs: %w", err)
			}

			for {
				entry, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				if entry.Timestamp != "" {
					fmt.Printf("%s %s\n", entry.Timestamp, entry.Message)
				} else {
					fmt.Println(entry.Message)
				}
			}
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	cmd.Flags().Int32VarP(&lines, "lines", "n", 100, "number of recent lines")
	return cmd
}

func deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a managed unit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.Delete(context.Background(), &pb.DeleteRequest{UnitName: args[0]})
			if err != nil {
				return fmt.Errorf("delete: %w", err)
			}
			fmt.Println(resp.Message)
			return nil
		},
	}
}

func printUnitStatus(u *pb.UnitStatus) {
	fmt.Printf("● %s\n", u.Name)
	fmt.Printf("  Type:     %s\n", strings.TrimPrefix(u.Type.String(), "UNIT_TYPE_"))
	fmt.Printf("  Desired:  %s\n", strings.TrimPrefix(u.DesiredState.String(), "DESIRED_STATE_"))
	fmt.Printf("  Active:   %s\n", strings.TrimPrefix(u.ActiveState.String(), "ACTIVE_STATE_"))
	fmt.Printf("  Enabled:  %v\n", u.Enabled)
	if len(u.ConfigFiles) > 0 {
		fmt.Printf("  Configs:  %s\n", strings.Join(u.ConfigFiles, ", "))
	}
	if u.Error != "" {
		fmt.Printf("  Error:    %s\n", u.Error)
	}
	fmt.Println()
}

func parseTypeFilter(s string) pb.UnitType {
	switch strings.ToLower(s) {
	case "container":
		return pb.UnitType_UNIT_TYPE_CONTAINER
	case "volume":
		return pb.UnitType_UNIT_TYPE_VOLUME
	case "network":
		return pb.UnitType_UNIT_TYPE_NETWORK
	default:
		return pb.UnitType_UNIT_TYPE_UNSPECIFIED
	}
}
