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

      executablePath = mkOption {
        type = types.nullOr types.str;
        default = null;
        description = ''
          Override the path to the bankshot executable used by the daemon service.
          Set this to a capability-wrapped binary path (e.g. /run/wrappers/bin/bankshot)
          to enable eBPF support. When null, uses the package binary directly.
        '';
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
        description = "Port ranges to automatically forward (applies to bankshot monitor on remote servers)";
      };

      ignoreProcesses = mkOption {
        type = types.listOf types.str;
        default = ["sshd" "systemd" "ssh-agent"];
        description = "Processes to ignore for port forwarding (applies to bankshot monitor on remote servers)";
      };

      pollInterval = mkOption {
        type = types.str;
        default = "5s";
        description = "Polling interval for process discovery (applies to bankshot monitor on remote servers)";
      };

      gracePeriod = mkOption {
        type = types.str;
        default = "30s";
        description = "Grace period before removing forwards after port close (applies to bankshot monitor on remote servers)";
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

  config = mkIf cfg.enable (let
    daemonExe = if cfg.daemon.executablePath != null
      then cfg.daemon.executablePath
      else "${cfg.package}/bin/bankshot";
    # When using a capability-wrapped binary for eBPF, we must disable all
    # mount-namespace sandboxing. User systemd instances lack CAP_SYS_ADMIN,
    # so any mount namespace feature (ProtectSystem, PrivateTmp, ProtectHome)
    # triggers CLONE_NEWUSER, placing the process in a non-init user namespace
    # where CAP_BPF is not recognized by the kernel.
    ebpfMode = cfg.daemon.executablePath != null;
  in {
    home.packages = [cfg.package]
      ++ lib.optional cfg.enableXdgOpen (pkgs.runCommand "bankshot-xdg-open" {} ''
        mkdir -p $out/bin
        ln -s ${cfg.package}/bin/bankshot $out/bin/xdg-open
      '');

    # Systemd user service for daemon
    systemd.user.services.bankshot-monitor = mkIf cfg.daemon.enable {
      Unit = {
        Description = "Bankshot monitor - automatic port forwarding";
        Documentation = "https://github.com/phinze/bankshot";
        After = ["network.target"];
        X-Restart-Triggers = [ "${cfg.package}" ];
      };

      Service = {
        Type = "notify";
        ExecStart = "${daemonExe} monitor run --systemd --log-level ${cfg.daemon.logLevel}";
        Restart = "on-failure";
        RestartSec = "5s";

        # Security hardening — disabled in eBPF mode (see ebpfMode comment above)
        PrivateTmp = !ebpfMode;
        ProtectSystem = if ebpfMode then false else "strict";
        ProtectHome = if ebpfMode then false else "read-only";
        ReadWritePaths = lib.mkIf (!ebpfMode) [
          "%h/.config/bankshot"
        ];

        # Resource limits
        MemoryMax = "256M";
        CPUQuota = "20%";
        # Allow eBPF memlock when using capability-wrapped binary
        LimitMEMLOCK = "infinity";

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
  });
}
