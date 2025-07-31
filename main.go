package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/phinze/bankshot/internal/logger"
	"github.com/phinze/bankshot/internal/process"
)

func main() {
	log := logger.Get()
	
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <command> [args...]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nWraps a command and automatically forwards any ports it binds via SSH.\n")
		os.Exit(1)
	}

	// Extract command and args
	command := os.Args[1]
	args := os.Args[2:]

	log.Info("starting bankshot", 
		slog.String("command", command),
		slog.Any("args", args),
	)

	// Create and start process
	pm := process.New(command, args)
	
	if err := pm.Start(); err != nil {
		log.Error("failed to start process", slog.String("error", err.Error()))
		os.Exit(1)
	}
	
	log.Debug("process started", slog.Int("pid", pm.PID()))
	
	// Wait for process to complete
	exitCode, err := pm.Wait()
	if err != nil {
		log.Error("process wait error", slog.String("error", err.Error()))
	}
	
	log.Info("process exited", slog.Int("exit_code", exitCode))
	os.Exit(exitCode)
}