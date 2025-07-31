package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/phinze/bankshot/internal/logger"
	"github.com/phinze/bankshot/internal/monitor"
	"github.com/phinze/bankshot/internal/process"
	"github.com/phinze/bankshot/internal/ssh"
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
	
	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Initialize SSH manager
	sshMgr, err := ssh.NewManager(log)
	if err != nil {
		log.Warn("SSH forwarding unavailable", slog.String("error", err.Error()))
		log.Info("continuing without port forwarding")
		// Continue without SSH forwarding
		sshMgr = nil
	}
	defer func() {
		if sshMgr != nil {
			sshMgr.Cleanup()
		}
	}()
	
	// Start port monitoring
	portMon := monitor.New(pm.PID(), log)
	if err := portMon.Start(ctx); err != nil {
		log.Error("failed to start port monitor", slog.String("error", err.Error()))
	}
	
	// Handle port events in background
	go handlePortEvents(ctx, portMon.Events(), sshMgr, log)
	
	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	
	// Wait for process to complete or signal
	done := make(chan struct{})
	var exitCode int
	
	go func() {
		code, err := pm.Wait()
		if err != nil {
			log.Error("process wait error", slog.String("error", err.Error()))
		}
		exitCode = code
		close(done)
	}()
	
	select {
	case <-done:
		// Process exited normally
		cancel() // Stop port monitoring
	case sig := <-sigChan:
		// Forward signal and wait for exit
		log.Debug("received signal", slog.String("signal", sig.String()))
		cancel() // Stop port monitoring
		<-done
	}
	
	log.Info("process exited", slog.Int("exit_code", exitCode))
	os.Exit(exitCode)
}

func handlePortEvents(ctx context.Context, events <-chan monitor.PortEvent, sshMgr *ssh.Manager, log *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			
			switch event.EventType {
			case monitor.PortOpened:
				if sshMgr != nil {
					if err := sshMgr.AddPortForward(event.Port.Port); err != nil {
						log.Error("failed to add port forward",
							slog.Int("port", event.Port.Port),
							slog.String("error", err.Error()),
						)
					}
				} else {
					log.Info("port opened (no SSH forwarding available)", 
						slog.Int("port", event.Port.Port),
						slog.String("protocol", event.Port.Protocol),
					)
				}
			case monitor.PortClosed:
				if sshMgr != nil {
					if err := sshMgr.RemovePortForward(event.Port.Port); err != nil {
						log.Error("failed to remove port forward",
							slog.Int("port", event.Port.Port),
							slog.String("error", err.Error()),
						)
					}
				} else {
					log.Info("port closed (no SSH forwarding available)", 
						slog.Int("port", event.Port.Port),
						slog.String("protocol", event.Port.Protocol),
					)
				}
			}
		}
	}
}