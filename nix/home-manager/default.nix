{
  config,
  lib,
  pkgs,
  bankshotPackages ? null,
  ...
}:
with lib; let
  cfg = config.programs.bankshot;
  yamlFormat = pkgs.formats.yaml {};
  
  # Generate the full config data structure
  configData = {
    network = "unix";
    address = "~/.bankshot.sock";
    log_level = cfg.daemon.logLevel;
    ssh_command = "ssh";
  } // {
    monitor = {
      portRanges = cfg.monitor.portRanges;
      ignoreProcesses = cfg.monitor.ignoreProcesses;
      pollInterval = cfg.monitor.pollInterval;
      gracePeriod = cfg.monitor.gracePeriod;
    };
  } // cfg.settings;
  
  configFile = yamlFormat.generate "bankshot-config.yaml" configData;
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

      logLevel = mkOption {
        type = types.enum ["debug" "info" "warn" "error"];
        default = "info";
        description = "Log level for the daemon";
      };
    };

    monitor = {
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
        default = [
          {
            start = 3000;
            end = 9999;
          }
        ];
        description = "Port ranges to automatically forward (applies to bankshotd on remote servers)";
      };

      ignoreProcesses = mkOption {
        type = types.listOf types.str;
        default = ["sshd" "systemd" "ssh-agent"];
        description = "Processes to ignore for port forwarding (applies to bankshotd on remote servers)";
      };

      pollInterval = mkOption {
        type = types.str;
        default = "5s";
        description = "Polling interval for process discovery (applies to bankshotd on remote servers)";
      };

      gracePeriod = mkOption {
        type = types.str;
        default = "30s";
        description = "Grace period before removing forwards after port close (applies to bankshotd on remote servers)";
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
          "%h/.config/bankshot"
        ];

        # Resource limits
        MemoryMax = "256M";
        CPUQuota = "20%";

        # Environment
        Environment = [
          "BANKSHOT_CONFIG=${configFile}"
        ];
      };

      Install = mkIf cfg.daemon.autoStart {
        WantedBy = ["default.target"];
      };
    };

    # Configuration file using pkgs.formats.yaml for proper formatting
    xdg.configFile."bankshot/config.yaml" = {
      source = configFile;
    };

    # Ensure ~/.local/bin is in PATH for xdg-open symlink
    home.sessionPath = mkIf cfg.enableXdgOpen ["$HOME/.local/bin"];
  };
}
