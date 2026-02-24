{
  description = "nebu-processor-registry - Community processors for the nebu data pipeline";

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
            # Go toolchain (using latest available)
            go
            gopls
            gotools
            go-tools

            # Python toolchain
            python312
            python312Packages.pip
            python312Packages.setuptools
            python312Packages.wheel

            # Protobuf tools
            protobuf
            protoc-gen-go
            protoc-gen-go-grpc

            # YAML tools
            yq-go

            # Build tools
            gnumake

            # Database tools
            duckdb

            # Utilities
            jq
            git
          ];

          shellHook = ''
            # Set GOTOOLCHAIN to auto to allow version switching
            export GOTOOLCHAIN=auto

            # Create a virtual environment for Python packages if it doesn't exist
            if [ ! -d .venv ]; then
              python -m venv .venv
            fi
            source .venv/bin/activate

            echo "🔧 nebu-processor-registry development environment"
            echo ""
            echo "Available tools:"
            echo "  go version: $(go version)"
            echo "  python version: $(python --version)"
            echo "  yq version: $(yq --version)"
            echo ""
            echo "Quick commands:"
            echo "  ./scripts/build-processor.sh processors/<name>  - Build a processor"
            echo "  ./scripts/validate-processor.sh processors/<name> - Validate processor metadata"
            echo "  pip install -e mcp/                              - Install MCP server"
            echo ""
          '';
        };
      }
    );
}
