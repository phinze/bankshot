# Bankshot Home Manager Module

This Home Manager module provides a declarative way to install and configure bankshot with optional xdg-open integration.

## Usage

Add bankshot to your flake inputs and use the module:

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    home-manager.url = "github:nix-community/home-manager";
    bankshot.url = "github:phinze/bankshot";
  };

  outputs = { nixpkgs, home-manager, bankshot, ... }: {
    homeConfigurations.myuser = home-manager.lib.homeManagerConfiguration {
      pkgs = nixpkgs.legacyPackages.x86_64-linux;
      modules = [
        bankshot.homeManagerModules.default
        {
          programs.bankshot = {
            enable = true;
            enableXdgOpen = true;  # Optional: symlink xdg-open to bankshot
            settings = {
              # Optional: configure bankshot settings
              debug = true;
            };
          };
        }
      ];
    };
  };
}
```

## Options

- `programs.bankshot.enable`: Enable bankshot
- `programs.bankshot.package`: The bankshot package to use (automatically provided when using the flake module)
- `programs.bankshot.enableXdgOpen`: Create an xdg-open symlink to bankshot in ~/.local/bin
- `programs.bankshot.settings`: Configuration to write to ~/.config/bankshot/config.json

## xdg-open Integration

When `enableXdgOpen` is set to `true`, the module will:

1. Create a symlink from `~/.local/bin/xdg-open` to the bankshot binary
2. Add `~/.local/bin` to your PATH

This allows bankshot to intercept browser open requests on remote machines where the real xdg-open might not work as expected.
