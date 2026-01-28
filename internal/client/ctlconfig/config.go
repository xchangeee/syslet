package ctlconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	CurrentContext string             `yaml:"current-context"`
	Contexts       map[string]Context `yaml:"contexts"`
}

type Context struct {
	Server string `yaml:"server"`
}

// ConfigPath returns ~/.config/rsctl/config.yaml, respecting XDG_CONFIG_HOME.
func ConfigPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "rsctl", "config.yaml")
}

// Load reads the config file. Returns an empty config if the file doesn't exist.
func Load() (*Config, error) {
	cfg := &Config{Contexts: make(map[string]Context)}
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = make(map[string]Context)
	}
	return cfg, nil
}

// Save writes the config file, creating parent directories as needed.
func Save(cfg *Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ServerAddr resolves the server address from the config given the current context.
// Returns empty string if no context is set or found.
func (c *Config) ServerAddr() string {
	if c.CurrentContext == "" {
		return ""
	}
	ctx, ok := c.Contexts[c.CurrentContext]
	if !ok {
		return ""
	}
	return ctx.Server
}

// ServerAddrForContext returns the server address for a named context.
func (c *Config) ServerAddrForContext(name string) (string, error) {
	ctx, ok := c.Contexts[name]
	if !ok {
		return "", fmt.Errorf("context %q not found", name)
	}
	return ctx.Server, nil
}
