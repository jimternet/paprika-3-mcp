package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/jimternet/paprika-3-mcp/internal/mcpserver"
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

func main() {
	username := flag.String("username", "", "Paprika 3 username (email). Falls back to $PAPRIKA_USERNAME.")
	password := flag.String("password", "", "Paprika 3 password. Falls back to $PAPRIKA_PASSWORD.")
	rateLimitMs := flag.Int("rate-limit-ms", 250, "Minimum milliseconds between API requests. Falls back to $PAPRIKA_RATE_LIMIT_MS.")
	showVersion := flag.Bool("version", false, "Print version and exit")
	cachePath := flag.String("cache-path", "", "Path to the recipe cache file. Falls back to platform default.")
	refreshInterval := flag.String("refresh-interval", "", "Background cache refresh interval (Go duration string, e.g. 5m). Falls back to $PAPRIKA_REFRESH_INTERVAL or 5m.")
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
	})
	if err != nil {
		logger.Error("failed to start paprika-3-mcp server", "err", err)
		os.Exit(1)
	}

	logger.Info("starting mcp server", "version", version, "cache_path", *cachePath)

	s.Start()
}
