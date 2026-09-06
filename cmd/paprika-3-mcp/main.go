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

	"github.com/soggycactus/paprika-3-mcp/internal/mcpserver"
	"gopkg.in/natefinch/lumberjack.v2"
)

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
	flag.Parse()

	if *showVersion {
		fmt.Printf("paprika-3-mcp version %s\n", version)
		os.Exit(0)
	}

	if *username == "" {
		*username = os.Getenv("PAPRIKA_USERNAME")
	}
	if *password == "" {
		*password = os.Getenv("PAPRIKA_PASSWORD")
	}
	if *rateLimitMs == 250 {
		if envVal := os.Getenv("PAPRIKA_RATE_LIMIT_MS"); envVal != "" {
			if ms, err := strconv.Atoi(envVal); err == nil && ms > 0 {
				*rateLimitMs = ms
			}
		}
	}

	if *username == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "username and password are required (set --username/--password flags or PAPRIKA_USERNAME/PAPRIKA_PASSWORD env vars)")
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
	})
	if err != nil {
		logger.Error("failed to start paprika-3-mcp server", "err", err)
		os.Exit(1)
	}

	logger.Info("starting mcp server", "version", version)

	s.Start()
}
