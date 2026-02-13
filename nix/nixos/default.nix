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
    ebpf = {
      enable = mkEnableOption ''
        eBPF capabilities for bankshot.
        Creates a security wrapper with CAP_BPF and CAP_PERFMON so the
        bankshot daemon can use eBPF-based port monitoring instead of polling.
        Set programs.bankshot.daemon.executablePath to the wrapper path
        (/run/wrappers/bin/bankshot) in your home-manager config.
      '';
    };

    package = mkOption {
      type = types.package;
      default =
        if bankshotPackages != null
        then bankshotPackages.${pkgs.system}.default
        else throw "bankshot package must be provided when not using the flake module";
      defaultText = literalExpression "bankshot.packages.\${system}.default";
      description = "The bankshot package to wrap with capabilities.";
    };
  };

  config = mkIf cfg.ebpf.enable {
    security.wrappers.bankshot = {
      source = "${cfg.package}/bin/bankshot";
      capabilities = "cap_bpf,cap_perfmon=ep";
      owner = "root";
      group = "root";
    };
  };
}
