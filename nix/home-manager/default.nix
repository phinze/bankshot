{
  config,
  lib,
  pkgs,
  bankshotPackages ? null,
  ...
}:
with lib; let
  cfg = config.programs.bankshot;
  configFile = pkgs.writeText "bankshot-config.yaml" (builtins.toJSON cfg.settings);
in {
  options.programs.bankshot = {
    enable = mkEnableOption "bankshot - automatic SSH port forwarding";

    package = mkOption {
      type = types.package;
      default =
        if bankshotPackages != null
        then bankshotPackages.${pkgs.system}.default
        else throw "bankshot package must be provided when not using the flake module";
      defaultText = literalExpression "bankshot.packages.\${system}.default";
      description = "The bankshot package to install.";
    };

    enableXdgOpen = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Whether to create an xdg-open symlink pointing to bankshot.
        This allows bankshot to handle browser opening on remote machines.
      '';
    };

    daemon = {
      enable = mkOption {
        type = types.bool;
        default = false;
        description = "Enable the bankshot daemon as a systemd user service";
      };

      autoStart = mkOption {
        type = types.bool;
        default = true;
        description = "Start the daemon automatically on user login";
      };

      socketActivation = mkOption {
        type = types.bool;
        default = true;
        description = "Use systemd socket activation for on-demand startup";
      };

      logLevel = mkOption {
        type = types.enum ["debug" "info" "warn" "error"];
        default = "info";
        description = "Log level for the daemon";
      };
    };

    monitor = {
      enable = mkOption {
        type = types.bool;
        default = false;
        description = "Enable automatic port monitoring in SSH sessions";
      };

      autoStart = mkOption {
        type = types.bool;
        default = true;
        description = "Start monitor automatically in SSH sessions via shell integration";
      };

      portRanges = mkOption {
        type = types.listOf (types.submodule {
          options = {
            start = mkOption { 
              type = types.int;
              description = "Start of port range";
            };
            end = mkOption { 
              type = types.int;
              description = "End of port range";
            };
          };
        });
        default = [{ start = 3000; end = 9999; }];
        description = "Port ranges to automatically forward";
      };

      ignoreProcesses = mkOption {
        type = types.listOf types.str;
        default = ["sshd" "systemd" "ssh-agent"];
        description = "Processes to ignore for port forwarding";
      };

      pollInterval = mkOption {
        type = types.str;
        default = "1s";
        description = "Polling interval for process discovery";
      };

      gracePeriod = mkOption {
        type = types.str;
        default = "30s";
        description = "Grace period before removing forwards after port close";
      };
    };

    settings = mkOption {
      type = types.attrsOf types.anything;
      default = {};
      example = literalExpression ''
        {
          debug = true;
          port = 8080;
        }
      '';
      description = ''
        Configuration settings for bankshot. These will be written to
        ~/.config/bankshot/config.yaml.
      '';
    };
  };

  config = mkIf cfg.enable {
    home.packages = [cfg.package];

    # Create xdg-open symlink if enabled
    home.file = mkIf cfg.enableXdgOpen {
      ".local/bin/xdg-open" = {
        source = "${cfg.package}/bin/bankshot";
      };
    };

    # Systemd user service for daemon
    systemd.user.services.bankshotd = mkIf cfg.daemon.enable {
      Unit = {
        Description = "Bankshotd automatic port forwarding daemon";
        Documentation = "https://github.com/phinze/bankshot";
        After = ["network.target"];
      } // optionalAttrs cfg.daemon.socketActivation {
        Requires = ["bankshotd.socket"];
      };

      Service = {
        Type = "notify";
        ExecStart = "${cfg.package}/bin/bankshot daemon run --systemd --log-level ${cfg.daemon.logLevel}";
        Restart = "on-failure";
        RestartSec = "5s";
        
        # Security hardening
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = "read-only";
        ReadWritePaths = [
          "%t/bankshotd"
          "%h/.config/bankshot"
        ];
        
        # Resource limits
        MemoryMax = "256M";
        CPUQuota = "20%";
        
        # Environment
        Environment = [
          "BANKSHOT_CONFIG=${configFile}"
          "BANKSHOT_RUNTIME_DIR=%t/bankshotd"
        ];
      };

      Install = mkIf cfg.daemon.autoStart {
        WantedBy = ["default.target"];
      };
    };

    # Systemd socket for daemon
    systemd.user.sockets.bankshotd = mkIf (cfg.daemon.enable && cfg.daemon.socketActivation) {
      Unit = {
        Description = "Bankshotd socket";
        Documentation = "https://github.com/phinze/bankshot";
      };

      Socket = {
        ListenStream = "%t/bankshotd/daemon.sock";
        RuntimeDirectory = "bankshotd";
        RuntimeDirectoryMode = "0700";
      };

      Install = {
        WantedBy = ["sockets.target"];
      };
    };

    # SSH session monitor service template
    systemd.user.services."bankshot-monitor@" = mkIf cfg.monitor.enable {
      Unit = {
        Description = "Bankshot port monitor for SSH session %i";
        BindsTo = ["bankshotd.service"];
        After = ["bankshotd.service"];
      };

      Service = {
        Type = "simple";
        ExecStart = "${cfg.package}/bin/bankshot monitor --session %i";
        Restart = "on-failure";
        RestartSec = "5s";
        
        # Pass monitor configuration
        Environment = [
          "BANKSHOT_MONITOR_POLL_INTERVAL=${cfg.monitor.pollInterval}"
          "BANKSHOT_MONITOR_GRACE_PERIOD=${cfg.monitor.gracePeriod}"
          "BANKSHOT_MONITOR_PORT_RANGES=${builtins.toJSON cfg.monitor.portRanges}"
          "BANKSHOT_MONITOR_IGNORE=${builtins.concatStringsSep "," cfg.monitor.ignoreProcesses}"
        ];
      };
    };

    # Shell integration for automatic monitor startup
    programs.bash.initExtra = mkIf (cfg.monitor.enable && cfg.monitor.autoStart) ''
      if [ -n "$SSH_CONNECTION" ] && command -v bankshot >/dev/null 2>&1; then
        # Generate unique session ID
        export BANKSHOT_SESSION="''${USER}-$$-$(date +%s)"
        
        # Start monitor for this session
        systemctl --user start "bankshot-monitor@$BANKSHOT_SESSION.service" 2>/dev/null || true
        
        # Cleanup on exit
        trap 'systemctl --user stop "bankshot-monitor@$BANKSHOT_SESSION.service" 2>/dev/null || true' EXIT
      fi
    '';

    programs.zsh.initExtra = mkIf (cfg.monitor.enable && cfg.monitor.autoStart) ''
      if [ -n "$SSH_CONNECTION" ] && command -v bankshot >/dev/null 2>&1; then
        # Generate unique session ID  
        export BANKSHOT_SESSION="''${USER}-$$-$(date +%s)"
        
        # Start monitor for this session
        systemctl --user start "bankshot-monitor@$BANKSHOT_SESSION.service" 2>/dev/null || true
        
        # Cleanup on exit
        zshexit() {
          systemctl --user stop "bankshot-monitor@$BANKSHOT_SESSION.service" 2>/dev/null || true
        }
      fi
    '';

    # Configuration file
    xdg.configFile."bankshot/config.yaml" = {
      text = builtins.toJSON ({
        network = "unix";
        address = "%t/bankshotd/daemon.sock";
        log_level = cfg.daemon.logLevel;
        monitor = {
          portRanges = cfg.monitor.portRanges;
          ignoreProcesses = cfg.monitor.ignoreProcesses;
          pollInterval = cfg.monitor.pollInterval;
          gracePeriod = cfg.monitor.gracePeriod;
        };
      } // cfg.settings);
    };

    # Ensure ~/.local/bin is in PATH for xdg-open symlink
    home.sessionPath = mkIf cfg.enableXdgOpen ["$HOME/.local/bin"];
  };
}
