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
        the bankshot monitor as a system service.

        The monitor needs CAP_BPF and CAP_PERFMON to watch ports over eBPF,
        and CAP_DAC_READ_SEARCH to read /proc for the processes that own
        them. systemd grants those to this one process through
        AmbientCapabilities, so ExecStart names the store path directly and
        the unit fully describes what it runs.

        Prefer this over the ebpf option below, which exists only because a
        systemd *user* service cannot hold capabilities at all
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

    ebpf = {
      enable = mkEnableOption ''
        eBPF capabilities for bankshot.
        Creates a security wrapper with CAP_BPF and CAP_PERFMON so the
        bankshot daemon can use eBPF-based port monitoring instead of polling.
        Set programs.bankshot.daemon.executablePath to the wrapper path
        (/run/wrappers/bin/bankshot) in your home-manager config.

        Note that this leaves a setcap copy of bankshot in /run/wrappers that
        any local user can exec, and that a unit pointed at that mutable path
        no longer names the binary it runs, so it can silently drift a build
        behind. services.bankshot.monitor above avoids both
      '';
    };

    package = mkOption {
      type = types.package;
      default =
        if bankshotPackages != null
        then bankshotPackages.${pkgs.stdenv.hostPlatform.system}.default
        else throw "bankshot package must be provided when not using the flake module";
      defaultText = literalExpression "bankshot.packages.\${system}.default";
      description = "The bankshot package to wrap with capabilities.";
    };
  };

  config = mkMerge [
    (mkIf cfg.ebpf.enable {
      security.wrappers.bankshot = {
        source = "${cfg.package}/bin/bankshot";
        capabilities = "cap_bpf,cap_perfmon,cap_dac_read_search=ep";
        owner = "root";
        group = "root";
      };
    })

    (mkIf cfg.monitor.enable {
      systemd.services.bankshot-monitor = {
        description = "Bankshot monitor - automatic port forwarding";
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

          AmbientCapabilities = ["CAP_BPF" "CAP_PERFMON" "CAP_DAC_READ_SEARCH"];
          CapabilityBoundingSet = ["CAP_BPF" "CAP_PERFMON" "CAP_DAC_READ_SEARCH"];

          # cilium/ebpf's RemoveMemlock() needs headroom the 8MB default
          # does not give it.
          LimitMEMLOCK = "infinity";

          MemoryMax = "256M";
          CPUQuota = "20%";
        };
      };
    })
  ];
}
