{
  config,
  lib,
  bankshotPackages ? null,
  pkgs,
  ...
}:
with lib; let
  cfg = config.services.bankshot;
in {
  options.services.bankshot = {
    monitor = {
      enable = mkEnableOption ''
        the bankshot monitor as a system service. It runs as the given user
        with no capabilities, and starts at boot rather than at login, so the
        ssh agent and the other jobs it carries are there before anyone
        attaches
      '';

      user = mkOption {
        type = types.str;
        example = "alice";
        description = ''
          User to run the monitor as. Forwards are requested on this user's
          behalf, and with no configFile set the monitor reads this user's
          ~/.config/bankshot/config.yaml.
        '';
      };

      configFile = mkOption {
        type = types.nullOr types.path;
        default = null;
        example = literalExpression "./bankshot-config.yaml";
        description = ''
          Config file to run against, exported as BANKSHOT_CONFIG. Naming a
          store path here also means a changed config changes the unit, so
          the monitor restarts onto it. When null the monitor falls back to
          the search path, starting at the user's XDG config directory.
        '';
      };

      logLevel = mkOption {
        type = types.enum ["debug" "info" "warn" "error"];
        default = "info";
        description = "Log level for the monitor.";
      };
    };

    package = mkOption {
      type = types.package;
      default =
        if bankshotPackages != null
        then bankshotPackages.${pkgs.stdenv.hostPlatform.system}.default
        else throw "bankshot package must be provided when not using the flake module";
      defaultText = literalExpression "bankshot.packages.\${system}.default";
      description = "The bankshot package to run.";
    };
  };

  config = mkMerge [
    (mkIf cfg.monitor.enable {
      systemd.services.bankshot-monitor = {
        description = "Bankshot monitor - carries requests to the laptop over the tailnet";
        documentation = ["https://github.com/phinze/bankshot"];
        wantedBy = ["multi-user.target"];
        # Tailnet forwarding asks tailscaled for this host's address and for
        # who each caller is. The monitor retries until the tailnet is up, so
        # this ordering only saves it a few failed attempts at boot.
        after = ["network.target"] ++ optional config.services.tailscale.enable "tailscaled.service";
        path = optional config.services.tailscale.enable config.services.tailscale.package;

        environment = optionalAttrs (cfg.monitor.configFile != null) {
          BANKSHOT_CONFIG = "${cfg.monitor.configFile}";
        };

        serviceConfig = {
          Type = "notify";
          User = cfg.monitor.user;
          ExecStart = "${cfg.package}/bin/bankshot monitor run --systemd --log-level ${cfg.monitor.logLevel}";
          Restart = "on-failure";
          RestartSec = "5s";

          CapabilityBoundingSet = "";

          MemoryMax = "256M";
          CPUQuota = "20%";
        };
      };
    })
  ];
}
