{
  config,
  lib,
  pkgs,
  bankshotPackages ? null,
  ...
}:
with lib; let
  cfg = config.programs.bankshot;
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
