package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/jimternet/paprika-3-mcp/internal/aisles"
	"github.com/jimternet/paprika-3-mcp/internal/mcpserver"
	"github.com/jimternet/paprika-3-mcp/internal/paprika"
	"gopkg.in/natefinch/lumberjack.v2"
)

// resolveCredentials applies the credential precedence chain:
//  1. CLI flags (already populated by flag.Parse)
//  2. Environment variables
//  3. macOS Keychain (darwin only; keychainPassword is a no-op elsewhere)
//
// logger may be nil during early startup; pass one only after it is
// initialised. The password is never included in any log message.
func resolveCredentials(username, password *string, logger *slog.Logger) {
	if *username == "" {
		*username = os.Getenv("PAPRIKA_USERNAME")
	}
	if *password == "" {
		*password = os.Getenv("PAPRIKA_PASSWORD")
	}

	// Keychain fallback: only attempted when username is known and password
	// is still missing.
	if *username != "" && *password == "" {
		pw, err := keychainPassword(*username)
		if err != nil {
			if logger != nil {
				logger.Debug("keychain lookup failed", "username", *username, "err", err)
			}
		} else if pw != "" {
			*password = pw
		}
	}
}

func getCachePath() string {
	switch runtime.GOOS {
	case "darwin": // macOS
		return filepath.Join(os.Getenv("HOME"), "Library", "Application Support", "paprika-3-mcp", "recipes.json")
	case "linux":
		if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
			return filepath.Join(xdg, "paprika-3-mcp", "recipes.json")
		}
		return filepath.Join(os.Getenv("HOME"), ".cache", "paprika-3-mcp", "recipes.json")
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "paprika-3-mcp", "recipes.json")
	default:
		return filepath.Join(os.TempDir(), "paprika-3-mcp", "recipes.json")
	}
}

var version = "dev" // set during build with -ldflags

func getLogFilePath() string {
	switch runtime.GOOS {
	case "darwin": // macOS
		return filepath.Join(os.Getenv("HOME"), "Library", "Logs", "paprika-3-mcp", "server.log")
	case "linux":
		return "/var/log/paprika-3-mcp/server.log"
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "paprika-3-mcp", "server.log")
	default:
		// fallback to /tmp for unknown OS
		return "/tmp/paprika-3-mcp/server.log"
	}
}

// handleAislesCmd dispatches the "aisles" subcommand.
func handleAislesCmd(args []string, cachePath, aislesConfigPath string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: paprika-3-mcp aisles <export|test|validate> [args...]")
		os.Exit(1)
	}

	switch args[0] {
	case "validate":
		cfg, warnings, err := aisles.Load(aislesConfigPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading aisles config: %v\n", err)
			os.Exit(1)
		}
		_ = cfg
		if len(warnings) == 0 {
			fmt.Println("aisles config OK — no warnings")
			return
		}
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "WARNING: %s\n", w)
		}
		os.Exit(1)

	case "test":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: paprika-3-mcp aisles test \"<ingredient>\"")
			os.Exit(1)
		}
		ingredient := strings.Join(args[1:], " ")

		cfg, warnings, err := aisles.Load(aislesConfigPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading aisles config: %v\n", err)
			os.Exit(1)
		}
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "WARNING: %s\n", w)
		}

		// Load cached aisles/history via a cache object (no network).
		c := paprika.NewCache(nil, cachePath, nil)
		_ = c.Load()
		c.SetAislesConfig(cfg)

		result := c.ResolveAisle(ingredient)

		fmt.Printf("Input:     %s\n", ingredient)
		fmt.Printf("Name:      %s\n", result.Name)
		fmt.Printf("Quantity:  %s\n", result.Quantity)
		fmt.Printf("Aisle:     %s\n", result.AisleName)
		fmt.Printf("AisleUID:  %s\n", result.AisleUID)
		fmt.Printf("Stage:     %s\n", result.Stage)

	case "export":
		exportFlags := flag.NewFlagSet("export", flag.ExitOnError)
		force := exportFlags.Bool("force", false, "overwrite existing config file")
		_ = exportFlags.Parse(args[1:])

		if _, err := os.Stat(aislesConfigPath); err == nil && !*force {
			fmt.Fprintf(os.Stderr, "config file already exists at %s; use --force to overwrite\n", aislesConfigPath)
			os.Exit(1)
		}

		// Write embedded default config as starter.
		cfg, _, err := aisles.Load("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading embedded defaults: %v\n", err)
			os.Exit(1)
		}

		// Build a minimal YAML with the aisle names from the embedded defaults.
		var sb strings.Builder
		sb.WriteString("# Paprika 3 MCP aisles configuration\n")
		sb.WriteString("# Edit keywords and modifiers to customize aisle assignment.\n\n")
		sb.WriteString("aisles:\n")
		for _, a := range cfg.Aisles {
			sb.WriteString(fmt.Sprintf("  - %s\n", a))
		}
		sb.WriteString("\n# modifiers and keywords inherit from embedded defaults.\n")
		sb.WriteString("# Add entries here to override or extend them.\n")
		sb.WriteString("modifiers: {}\n")
		sb.WriteString("keywords: {}\n")

		if err := os.MkdirAll(filepath.Dir(aislesConfigPath), 0700); err != nil {
			fmt.Fprintf(os.Stderr, "error creating config dir: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(aislesConfigPath, []byte(sb.String()), 0600); err != nil {
			fmt.Fprintf(os.Stderr, "error writing config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Wrote starter aisles config to %s\n", aislesConfigPath)

	default:
		fmt.Fprintf(os.Stderr, "unknown aisles subcommand %q; use export, test, or validate\n", args[0])
		os.Exit(1)
	}
}

func main() {
	// Subcommand dispatch: if the first argument is "aisles", handle it separately.
	// We need to do this before flag.Parse so that aisles subcommands get their own flags.
	if len(os.Args) > 1 && os.Args[1] == "aisles" {
		// Resolve cache path early for the subcommand.
		cachePath := os.Getenv("PAPRIKA_CACHE_PATH")
		if cachePath == "" {
			cachePath = getCachePath()
		}
		aislesConfigPath := os.Getenv("PAPRIKA_AISLES_CONFIG")
		if aislesConfigPath == "" {
			aislesConfigPath = filepath.Join(filepath.Dir(cachePath), "aisles.yaml")
		}
		handleAislesCmd(os.Args[2:], cachePath, aislesConfigPath)
		return
	}

	username := flag.String("username", "", "Paprika 3 username (email). Falls back to $PAPRIKA_USERNAME.")
	password := flag.String("password", "", "Paprika 3 password. Falls back to $PAPRIKA_PASSWORD.")
	rateLimitMs := flag.Int("rate-limit-ms", 250, "Minimum milliseconds between API requests. Falls back to $PAPRIKA_RATE_LIMIT_MS.")
	showVersion := flag.Bool("version", false, "Print version and exit")
	cachePath := flag.String("cache-path", "", "Path to the recipe cache file. Falls back to platform default.")
	refreshInterval := flag.String("refresh-interval", "", "Background cache refresh interval (Go duration string, e.g. 5m). Falls back to $PAPRIKA_REFRESH_INTERVAL or 5m.")
	aislesConfig := flag.String("aisles-config", "", "Path to aisles YAML config file. Falls back to $PAPRIKA_AISLES_CONFIG or <cache-dir>/aisles.yaml.")
	flag.Parse()

	if *showVersion {
		fmt.Printf("paprika-3-mcp version %s\n", version)
		os.Exit(0)
	}

	// Resolve credentials: flags > env vars > Keychain.
	// Logger not yet initialised here; pass nil so Keychain debug messages
	// go nowhere rather than panic. They will be visible once the file
	// logger is set up, but a failed Keychain lookup at startup is not fatal.
	resolveCredentials(username, password, nil)

	if *rateLimitMs == 250 {
		if envVal := os.Getenv("PAPRIKA_RATE_LIMIT_MS"); envVal != "" {
			if ms, err := strconv.Atoi(envVal); err == nil && ms > 0 {
				*rateLimitMs = ms
			}
		}
	}
	if *cachePath == "" {
		*cachePath = getCachePath()
	}

	// Resolve aisles config path: flag > env > default (<cache-dir>/aisles.yaml).
	if *aislesConfig == "" {
		*aislesConfig = os.Getenv("PAPRIKA_AISLES_CONFIG")
	}
	if *aislesConfig == "" {
		*aislesConfig = filepath.Join(filepath.Dir(*cachePath), "aisles.yaml")
	}

	// Resolve refresh interval: flag > env > default.
	var parsedRefreshInterval time.Duration
	if *refreshInterval == "" {
		*refreshInterval = os.Getenv("PAPRIKA_REFRESH_INTERVAL")
	}
	if *refreshInterval != "" {
		d, err := time.ParseDuration(*refreshInterval)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid --refresh-interval value %q: %v\n", *refreshInterval, err)
			os.Exit(1)
		}
		parsedRefreshInterval = d
	}
	// 0 means "use default" inside NewServer.

	if *username == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "username and password are required (set --username/--password flags, PAPRIKA_USERNAME/PAPRIKA_PASSWORD env vars, or store the password in the macOS Keychain with: security add-generic-password -s paprika-3-mcp -a <username> -w)")
		os.Exit(1)
	}

	rateLimitInterval := time.Duration(*rateLimitMs) * time.Millisecond

	logFile := getLogFilePath()
	writer := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    100,  // megabytes
		MaxBackups: 5,    // keep 5 old log files
		MaxAge:     10,   // days
		Compress:   true, // gzip old logs
	}

	logger := slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	s, err := mcpserver.NewServer(mcpserver.NewServerOptions{
		Version:           version,
		Username:          *username,
		Password:          *password,
		Logger:            logger,
		RateLimitInterval: rateLimitInterval,
		CachePath:         *cachePath,
		RefreshInterval:   parsedRefreshInterval,
		AislesConfigPath:  *aislesConfig,
	})
	if err != nil {
		logger.Error("failed to start paprika-3-mcp server", "err", err)
		os.Exit(1)
	}

	logger.Info("starting mcp server", "version", version, "cache_path", *cachePath, "aisles_config", *aislesConfig)

	s.Start()
}
