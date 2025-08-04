{
  description = "bankshot - automatic SSH port forwarding for remote development";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
  }:
    {
      # Home Manager module
      homeManagerModules.default = { pkgs, ... }: {
        imports = [ ./nix/home-manager ];
        config._module.args.bankshotPackages = self.packages;
      };
      homeManagerModules.bankshot = self.homeManagerModules.default;
    }
    // flake-utils.lib.eachDefaultSystem (system: let
      pkgs = nixpkgs.legacyPackages.${system};
    in {
      # Note: This flake provides a Nix package for bankshot.
      # Official releases are built using goreleaser (.goreleaser.yml).
      # This package definition ensures Nix users can install bankshot
      # with consistent build flags and behavior.
      packages = rec {
        bankshot = pkgs.buildGoModule rec {
          pname = "bankshot";
          version =
            if (self ? lastModifiedDate && self ? shortRev)
            then "${builtins.substring 0 8 self.lastModifiedDate}-${self.shortRev}"
            else if (self ? lastModifiedDate)
            then builtins.substring 0 8 self.lastModifiedDate
            else "dev";

          src = ./.;

          vendorHash = "sha256-hk8r/K4AA1Bt/ghEFt0oRYb1zm2SFfoI0R2Yik4SoIs=";

          # Make ssh available for tests
          nativeBuildInputs = [pkgs.openssh];

          # Disable CGO to match goreleaser builds
          env.CGO_ENABLED = "0";

          # Build flags aligned with goreleaser configuration
          ldflags = [
            "-s"
            "-w" # Strip debug info to match goreleaser
            "-X github.com/phinze/bankshot/version.Version=${version}"
            "-X github.com/phinze/bankshot/version.Commit=${self.shortRev or self.dirtyShortRev or "unknown"}"
            "-X github.com/phinze/bankshot/version.Date=${self.lastModifiedDate or "1970-01-01"}"
            "-X github.com/phinze/bankshot/version.BuiltBy=nix"
          ];

          # Build both binaries
          subPackages = ["cmd/bankshot" "cmd/bankshotd"];

          meta = with pkgs.lib; {
            description = "Automatic SSH port forwarding for remote development";
            homepage = "https://github.com/phinze/bankshot";
            license = licenses.mit;
            maintainers = [];
            mainProgram = "bankshot";
          };
        };
        default = bankshot;
      };

      apps.default = flake-utils.lib.mkApp {
        drv = self.packages.${system}.default;
      };

      devShells.default = pkgs.mkShell {
        buildInputs = with pkgs; [
          go_1_24
          gopls
          gotools
          go-tools
          golangci-lint
          delve
          goreleaser
          svu
        ];

        shellHook = ''
          echo "bankshot development environment"
          echo "Go version: $(go version)"
        '';
      };
    });
}
