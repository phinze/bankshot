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
  } // lib.optionalAttrs (cfg.notifyPackage != null) {
    notify_command = "${config.home.homeDirectory}/Applications/BankshotNotify.app/Contents/MacOS/bankshot-notify";
  } // {
    monitor = {
      portRanges = cfg.monitor.portRanges;
      ignorePorts = cfg.monitor.ignorePorts;
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

    notifyPackage = mkOption {
      type = types.nullOr types.package;
      default =
        if bankshotPackages != null && (bankshotPackages.${pkgs.system} ? bankshot-notify)
        then bankshotPackages.${pkgs.system}.bankshot-notify
        else null;
      defaultText = literalExpression "bankshot-notify on darwin, null otherwise";
      description = ''
        The bankshot-notify package for native desktop notifications.
        Set to null to disable notifications. The app bundle is copied to
        ~/Applications and code-signed at activation time.
      '';
    };

    notifySigningIdentity = mkOption {
      type = types.nullOr types.str;
      default = null;
      example = "Developer ID Application: Jane Smith (TEAMID)";
      description = ''
        Code signing identity for the notification helper app.
        When null (default), tries to find a Developer ID certificate
        in the keychain and falls back to ad-hoc signing.
      '';
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
        default = [];
        description = ''
          Port ranges to automatically forward. When empty (default), all
          non-privileged ports (>= 1024) are forwarded. Set explicitly to
          restrict to specific ranges.
        '';
      };

      ignorePorts = mkOption {
        type = types.listOf types.int;
        default = [];
        description = "Specific ports to never auto-forward (applies to bankshot monitor on remote servers)";
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

    # Install and code-sign the notification helper app bundle.
    # UNUserNotificationCenter requires a signed app bundle to allow
    # notification authorization. We copy to ~/Applications so the
    # bundle is mutable (Nix store is read-only) and sign it there.
    home.activation.bankshotNotify = lib.mkIf (cfg.notifyPackage != null) (
      lib.hm.dag.entryAfter ["writeBoundary"] ''
        app_src="${cfg.notifyPackage}/Applications/BankshotNotify.app"
        app_dst="$HOME/Applications/BankshotNotify.app"

        # Copy app bundle (overwrite previous version)
        mkdir -p "$HOME/Applications"
        rm -rf "$app_dst"
        cp -R "$app_src" "$app_dst"
        chmod -R u+w "$app_dst"

        # Sign: use explicit identity, or auto-detect Developer ID, or ad-hoc
        identity=""
        ${lib.optionalString (cfg.notifySigningIdentity != null) ''
          identity=${lib.escapeShellArg cfg.notifySigningIdentity}
        ''}
        if [ -z "$identity" ]; then
          # Try to find a signing certificate (prefer Developer ID, then Apple Development)
          for pattern in "Developer ID Application" "Apple Development"; do
            if /usr/bin/security find-identity -v -p codesigning 2>/dev/null \
               | grep -q "$pattern"; then
              identity=$(/usr/bin/security find-identity -v -p codesigning 2>/dev/null \
                | grep "$pattern" | head -1 \
                | sed 's/.*"\(.*\)".*/\1/')
              break
            fi
          done
        fi

        if [ -n "$identity" ]; then
          run /usr/bin/codesign --force --deep -s "$identity" "$app_dst"
        else
          run /usr/bin/codesign --force --deep -s - "$app_dst"
        fi
      ''
    );

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
