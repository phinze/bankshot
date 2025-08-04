{
  config,
  lib,
  pkgs,
  ...
}:
with lib; let
  cfg = config.programs.bankshot;

  # Import bankshot from the flake if not available in pkgs
  bankshotPackage =
    if (pkgs ? bankshot)
    then pkgs.bankshot
    else (builtins.getFlake (toString ../..)).packages.${pkgs.system}.bankshot;
in {
  options.programs.bankshot = {
    enable = mkEnableOption "bankshot - automatic SSH port forwarding";

    package = mkOption {
      type = types.package;
      default = bankshotPackage;
      defaultText = literalExpression "pkgs.bankshot";
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
        ~/.config/bankshot/config.json.
      '';
    };
  };

  config = mkIf cfg.enable {
    home.packages = [cfg.package];

    # Create xdg-open symlink if enabled
    home.file = mkIf cfg.enableXdgOpen {
      ".local/bin/xdg-open" = {
        source = "${cfg.package}/bin/bankshot";
        executable = true;
      };
    };

    # Create config file if settings are provided
    xdg.configFile = mkIf (cfg.settings != {}) {
      "bankshot/config.json" = {
        text = builtins.toJSON cfg.settings;
      };
    };

    # Ensure ~/.local/bin is in PATH for xdg-open symlink
    home.sessionPath = mkIf cfg.enableXdgOpen ["$HOME/.local/bin"];
  };
}

