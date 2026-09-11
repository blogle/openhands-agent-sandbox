{
  description = "openhands-agent-sandbox-runtime development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            golangci-lint
            gotools
            kubectl
            kustomize
            jq
            curl
            git
            gnumake
          ];

          shellHook = ''
            echo "openhands-agent-sandbox-runtime dev shell"
            echo "Go: $(go version)"
            echo "Run 'make test' to run unit tests"
            echo "Run 'make lint' to run linters"
            echo "Run 'make build' to build the binary"
          '';
        };
      });
}
