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
  } // lib.optionalAttrs (cfg.notifyPackage != null) {
    notify_command = "${config.home.homeDirectory}/Applications/BankshotNotify.app/Contents/MacOS/bankshot-notify";
  } // lib.optionalAttrs cfg.opProxy.enabled {
    op_proxy = {
      enabled = cfg.opProxy.enabled;
      op_path = cfg.opProxy.opPath;
      allowed_vaults = cfg.opProxy.allowedVaults;
      read_only = cfg.opProxy.readOnly;
    } // lib.optionalAttrs (cfg.opProxy.account != null) {
      account = cfg.opProxy.account;
    };
  } // cfg.settings;
  
  configFile = yamlFormat.generate "bankshot-config.yaml" configData;
in {
  options.programs.bankshot = {
    enable = mkEnableOption "bankshot - open URLs, paste, op and ssh-agent from remote hosts, and forward ports on request";

    package = mkOption {
      type = types.package;
      default =
        if bankshotPackages != null
        then bankshotPackages.${pkgs.stdenv.hostPlatform.system}.default
        else throw "bankshot package must be provided when not using the flake module";
      defaultText = literalExpression "bankshot.packages.\${system}.default";
      description = "The bankshot package to install.";
    };

    notifyPackage = mkOption {
      type = types.nullOr types.package;
      default =
        if bankshotPackages != null && (bankshotPackages.${pkgs.stdenv.hostPlatform.system} ? bankshot-notify)
        then bankshotPackages.${pkgs.stdenv.hostPlatform.system}.bankshot-notify
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

    enableOpProxy = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Whether to create an op symlink pointing to bankshot.
        This allows bankshot to proxy 1Password CLI requests to the local
        machine where native app integration (Touch ID) is available.
      '';
    };

    enableWlPaste = mkOption {
      type = types.bool;
      default = false;
      description = ''
        Whether to create a wl-paste symlink pointing to bankshot.
        This lets tools that read clipboard images with wl-paste (Claude
        Code's image paste on Linux) get the image on the local machine's
        clipboard. Only images are served, never text.
      '';
    };

    opProxy = {
      enabled = mkOption {
        type = types.bool;
        default = false;
        description = "Enable the op-proxy handler in the daemon";
      };

      opPath = mkOption {
        type = types.str;
        default = "op";
        description = "Path to the op binary on the local (daemon) machine";
      };

      allowedVaults = mkOption {
        type = types.listOf types.str;
        default = [];
        description = ''
          Vaults that may be accessed via op-proxy. When empty, all vaults
          are allowed. Set explicitly to restrict access.
        '';
      };

      account = mkOption {
        type = types.nullOr types.str;
        default = null;
        example = "my.1password.com";
        description = ''
          1Password account op runs against (shorthand, sign-in address, or
          ID), passed as OP_ACCOUNT. Needed when the local machine is signed
          in to more than one account; requests can't pick another.
        '';
      };

      readOnly = mkOption {
        type = types.bool;
        default = true;
        description = ''
          When true (default), only read-only op subcommands are allowed
          (item get, item list, read, whoami). Set to false to also allow
          write operations (item create, item edit, item delete, run, inject).
        '';
      };
    };

    daemon = {
      enable = mkOption {
        type = types.bool;
        default = false;
        description = "Enable the bankshot daemon service (launchd on macOS, systemd on Linux)";
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

    generatedConfig = mkOption {
      type = types.path;
      readOnly = true;
      default = configFile;
      defaultText = literalExpression "<generated config file>";
      description = ''
        The rendered config file, in the store. Exposed so something outside
        home-manager can be pointed at the settings this module already
        renders instead of describing them a second time. NixOS hosts wire it
        into services.bankshot.monitor.configFile, which both feeds the
        monitor its config and makes a config change restart it.
      '';
    };
  };

  config = mkIf cfg.enable {
    home.packages = [cfg.package]
      ++ lib.optional cfg.enableXdgOpen (pkgs.runCommand "bankshot-xdg-open" {} ''
        mkdir -p $out/bin
        ln -s ${cfg.package}/bin/bankshot $out/bin/xdg-open
      '')
      ++ lib.optional cfg.enableOpProxy (pkgs.runCommand "bankshot-op-proxy" {} ''
        mkdir -p $out/bin
        ln -s ${cfg.package}/bin/bankshot $out/bin/op
      '')
      ++ lib.optional cfg.enableWlPaste (pkgs.runCommand "bankshot-wl-paste" {} ''
        mkdir -p $out/bin
        ln -s ${cfg.package}/bin/bankshot $out/bin/wl-paste
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

    # Launchd agent for daemon (macOS)
    launchd.agents.bankshot = mkIf (cfg.daemon.enable && pkgs.stdenv.hostPlatform.isDarwin) {
      enable = true;
      config = {
        ProgramArguments = [
          "${cfg.package}/bin/bankshotd"
          "--config" "${configFile}"
        ];
        KeepAlive = true;
        RunAtLoad = cfg.daemon.autoStart;
        StandardOutPath = "${config.home.homeDirectory}/Library/Logs/bankshotd.log";
        StandardErrorPath = "${config.home.homeDirectory}/Library/Logs/bankshotd.error.log";
      };
    };

    # Systemd user service for monitor (Linux remote servers)
    systemd.user.services.bankshot-monitor = mkIf (cfg.daemon.enable && pkgs.stdenv.hostPlatform.isLinux) {
      Unit = {
        Description = "Bankshot monitor - carries requests to the laptop over the tailnet";
        Documentation = "https://github.com/phinze/bankshot";
        After = ["network.target"];
        X-Restart-Triggers = [ "${cfg.package}" ];
      };

      Service = {
        Type = "notify";
        ExecStart = "${cfg.package}/bin/bankshot monitor run --systemd --log-level ${cfg.daemon.logLevel}";
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
  };
}
