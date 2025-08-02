{
  description = "bankshot - automatic SSH port forwarding for remote development";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go_1_24
            gopls
            gotools
            go-tools
            golangci-lint
            delve
            goreleaser
          ];

          shellHook = ''
            echo "bankshot development environment"
            echo "Go version: $(go version)"
          '';
        };
      });
}