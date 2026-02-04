package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pb "codeberg.org/xchangeee/syslet/proto"

	"codeberg.org/xchangeee/syslet/internal/client/ctlconfig"

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
	root.AddCommand(getCmd())
	root.AddCommand(listCmd())
	root.AddCommand(deleteCmd())
	root.AddCommand(logsCmd())
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

// ---------------------------------------------------------------------------
// apply
// ---------------------------------------------------------------------------

type jsonSpec struct {
	Name         string                    `json:"name"`
	Type         string                    `json:"type"`
	DesiredState string                    `json:"desiredState"`
	Unit         map[string]map[string]any `json:"unit"`
	Configs      []jsonConfigEntry         `json:"configs,omitempty"`
}

type jsonConfigEntry struct {
	Content          string `json:"content"`
	TargetVolumePath string `json:"targetVolumePath"`
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

			rawSpecs, err := loadSpecs(filePath)
			if err != nil {
				return err
			}

			// Parse and group by type.
			var containers []*pb.ContainerSpec
			var volumes []*pb.VolumeSpec
			var networks []*pb.NetworkSpec

			for _, raw := range rawSpecs {
				var js jsonSpec
				if err := json.Unmarshal([]byte(raw), &js); err != nil {
					return fmt.Errorf("parsing spec JSON: %w", err)
				}
				switch strings.ToLower(js.Type) {
				case "container":
					spec, err := toContainerSpec(js)
					if err != nil {
						return fmt.Errorf("container %q: %w", js.Name, err)
					}
					containers = append(containers, spec)
				case "volume":
					spec, err := toVolumeSpec(js)
					if err != nil {
						return fmt.Errorf("volume %q: %w", js.Name, err)
					}
					volumes = append(volumes, spec)
				case "network":
					spec, err := toNetworkSpec(js)
					if err != nil {
						return fmt.Errorf("network %q: %w", js.Name, err)
					}
					networks = append(networks, spec)
				default:
					return fmt.Errorf("unknown type %q for spec %q", js.Type, js.Name)
				}
			}

			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			// Apply in dependency order: networks → volumes → containers.
			var allResults []*pb.ApplyResult

			if len(networks) > 0 {
				resp, err := client.ApplyNetworks(context.Background(), &pb.ApplyNetworksRequest{Specs: networks})
				if err != nil {
					return fmt.Errorf("apply networks: %w", err)
				}
				allResults = append(allResults, resp.Results...)
			}

			if len(volumes) > 0 {
				resp, err := client.ApplyVolumes(context.Background(), &pb.ApplyVolumesRequest{Specs: volumes})
				if err != nil {
					return fmt.Errorf("apply volumes: %w", err)
				}
				allResults = append(allResults, resp.Results...)
			}

			if len(containers) > 0 {
				resp, err := client.ApplyContainers(context.Background(), &pb.ApplyContainersRequest{Specs: containers})
				if err != nil {
					return fmt.Errorf("apply containers: %w", err)
				}
				allResults = append(allResults, resp.Results...)
			}

			for _, r := range allResults {
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

// flattenUnitOptions converts the JSON "unit" map to sorted proto UnitOptions.
func flattenUnitOptions(unitMap map[string]map[string]any) ([]*pb.UnitOption, error) {
	sections := make([]string, 0, len(unitMap))
	for s := range unitMap {
		sections = append(sections, s)
	}
	sort.Strings(sections)

	var opts []*pb.UnitOption
	for _, section := range sections {
		keys := make([]string, 0, len(unitMap[section]))
		for k := range unitMap[section] {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, key := range keys {
			val := unitMap[section][key]
			switch v := val.(type) {
			case string:
				opts = append(opts, &pb.UnitOption{
					Section: section,
					Name:    key,
					Value:   v,
				})
			case []any:
				for _, item := range v {
					s, ok := item.(string)
					if !ok {
						return nil, fmt.Errorf("option %s.%s: expected string value, got %T", section, key, item)
					}
					opts = append(opts, &pb.UnitOption{
						Section: section,
						Name:    key,
						Value:   s,
					})
				}
			default:
				opts = append(opts, &pb.UnitOption{
					Section: section,
					Name:    key,
					Value:   fmt.Sprintf("%v", v),
				})
			}
		}
	}
	return opts, nil
}

func toContainerSpec(js jsonSpec) (*pb.ContainerSpec, error) {
	opts, err := flattenUnitOptions(js.Unit)
	if err != nil {
		return nil, err
	}

	spec := &pb.ContainerSpec{
		Name:    js.Name,
		Options: opts,
	}

	switch strings.ToLower(js.DesiredState) {
	case "running":
		spec.DesiredState = pb.DesiredState_DESIRED_STATE_RUNNING
	case "stopped":
		spec.DesiredState = pb.DesiredState_DESIRED_STATE_STOPPED
	case "":
		// no desired state specified
	default:
		return nil, fmt.Errorf("unknown desired state: %q", js.DesiredState)
	}

	for _, ce := range js.Configs {
		spec.Configs = append(spec.Configs, &pb.ConfigEntry{
			Content:          ce.Content,
			TargetVolumePath: ce.TargetVolumePath,
		})
	}

	return spec, nil
}

func toVolumeSpec(js jsonSpec) (*pb.VolumeSpec, error) {
	opts, err := flattenUnitOptions(js.Unit)
	if err != nil {
		return nil, err
	}
	return &pb.VolumeSpec{
		Name:    js.Name,
		Options: opts,
	}, nil
}

func toNetworkSpec(js jsonSpec) (*pb.NetworkSpec, error) {
	opts, err := flattenUnitOptions(js.Unit)
	if err != nil {
		return nil, err
	}
	return &pb.NetworkSpec{
		Name:    js.Name,
		Options: opts,
	}, nil
}

// ---------------------------------------------------------------------------
// get
// ---------------------------------------------------------------------------

func getCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get status of a resource",
	}
	cmd.AddCommand(getContainerCmd())
	cmd.AddCommand(getVolumeCmd())
	cmd.AddCommand(getNetworkCmd())
	return cmd
}

func getContainerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "container <name>",
		Short: "Get status of a container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.GetContainer(context.Background(), &pb.GetContainerRequest{Name: args[0]})
			if err != nil {
				return fmt.Errorf("get container: %w", err)
			}
			printContainerStatus(resp.Container)
			return nil
		},
	}
}

func getVolumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "volume <name>",
		Short: "Get status of a volume",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.GetVolume(context.Background(), &pb.GetVolumeRequest{Name: args[0]})
			if err != nil {
				return fmt.Errorf("get volume: %w", err)
			}
			printVolumeStatus(resp.Volume)
			return nil
		},
	}
}

func getNetworkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "network <name>",
		Short: "Get status of a network",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.GetNetwork(context.Background(), &pb.GetNetworkRequest{Name: args[0]})
			if err != nil {
				return fmt.Errorf("get network: %w", err)
			}
			printNetworkStatus(resp.Network)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List managed resources",
	}
	cmd.AddCommand(listContainersCmd())
	cmd.AddCommand(listVolumesCmd())
	cmd.AddCommand(listNetworksCmd())
	return cmd
}

func listContainersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "containers",
		Short: "List managed containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.ListContainers(context.Background(), &pb.ListContainersRequest{})
			if err != nil {
				return fmt.Errorf("list containers: %w", err)
			}

			fmt.Printf("%-30s %-10s %-8s\n", "NAME", "ACTIVE", "ENABLED")
			for _, c := range resp.Containers {
				fmt.Printf("%-30s %-10s %-8v\n",
					c.Name,
					strings.TrimPrefix(c.ActiveState.String(), "ACTIVE_STATE_"),
					c.Enabled,
				)
			}
			return nil
		},
	}
}

func listVolumesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "volumes",
		Short: "List managed volumes",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.ListVolumes(context.Background(), &pb.ListVolumesRequest{})
			if err != nil {
				return fmt.Errorf("list volumes: %w", err)
			}

			fmt.Printf("%-30s %-10s %-8s\n", "NAME", "ACTIVE", "ENABLED")
			for _, v := range resp.Volumes {
				fmt.Printf("%-30s %-10s %-8v\n",
					v.Name,
					strings.TrimPrefix(v.ActiveState.String(), "ACTIVE_STATE_"),
					v.Enabled,
				)
			}
			return nil
		},
	}
}

func listNetworksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "networks",
		Short: "List managed networks",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.ListNetworks(context.Background(), &pb.ListNetworksRequest{})
			if err != nil {
				return fmt.Errorf("list networks: %w", err)
			}

			fmt.Printf("%-30s %-10s %-8s\n", "NAME", "ACTIVE", "ENABLED")
			for _, n := range resp.Networks {
				fmt.Printf("%-30s %-10s %-8v\n",
					n.Name,
					strings.TrimPrefix(n.ActiveState.String(), "ACTIVE_STATE_"),
					n.Enabled,
				)
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

func deleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a managed resource",
	}
	cmd.AddCommand(deleteContainerCmd())
	cmd.AddCommand(deleteVolumeCmd())
	cmd.AddCommand(deleteNetworkCmd())
	return cmd
}

func deleteContainerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "container <name>",
		Short: "Delete a managed container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.DeleteContainer(context.Background(), &pb.DeleteContainerRequest{Name: args[0]})
			if err != nil {
				return fmt.Errorf("delete container: %w", err)
			}
			fmt.Println(resp.Message)
			return nil
		},
	}
}

func deleteVolumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "volume <name>",
		Short: "Delete a managed volume",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.DeleteVolume(context.Background(), &pb.DeleteVolumeRequest{Name: args[0]})
			if err != nil {
				return fmt.Errorf("delete volume: %w", err)
			}
			fmt.Println(resp.Message)
			return nil
		},
	}
}

func deleteNetworkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "network <name>",
		Short: "Delete a managed network",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := connect()
			if err != nil {
				return err
			}
			defer conn.Close()

			resp, err := client.DeleteNetwork(context.Background(), &pb.DeleteNetworkRequest{Name: args[0]})
			if err != nil {
				return fmt.Errorf("delete network: %w", err)
			}
			fmt.Println(resp.Message)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// logs
// ---------------------------------------------------------------------------

func logsCmd() *cobra.Command {
	var follow bool
	var lines int32

	cmd := &cobra.Command{
		Use:   "logs <container>",
		Short: "Stream journal logs for a container",
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

// ---------------------------------------------------------------------------
// status printers
// ---------------------------------------------------------------------------

func printContainerStatus(c *pb.ContainerStatus) {
	fmt.Printf("● %s\n", c.Name)
	fmt.Printf("  Active:   %s\n", strings.TrimPrefix(c.ActiveState.String(), "ACTIVE_STATE_"))
	fmt.Printf("  Enabled:  %v\n", c.Enabled)
	if len(c.ConfigFiles) > 0 {
		fmt.Printf("  Configs:  %s\n", strings.Join(c.ConfigFiles, ", "))
	}
	fmt.Println()
}

func printVolumeStatus(v *pb.VolumeStatus) {
	fmt.Printf("● %s\n", v.Name)
	fmt.Printf("  Active:   %s\n", strings.TrimPrefix(v.ActiveState.String(), "ACTIVE_STATE_"))
	fmt.Printf("  Enabled:  %v\n", v.Enabled)
	fmt.Println()
}

func printNetworkStatus(n *pb.NetworkStatus) {
	fmt.Printf("● %s\n", n.Name)
	fmt.Printf("  Active:   %s\n", strings.TrimPrefix(n.ActiveState.String(), "ACTIVE_STATE_"))
	fmt.Printf("  Enabled:  %v\n", n.Enabled)
	fmt.Println()
}
