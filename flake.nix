{
  description = "glt - a fast terminal UI for GitLab merge requests and pipelines";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        version = "0.1.0";
      in
      {
        packages = rec {
          glt = pkgs.buildGoModule {
            pname = "glt";
            inherit version;
            src = ./.;
            vendorHash = "sha256-HzVcM0rfas2GBnbIYlVj3ZkVgAiSehYaUapRcdtOYds=";
            subPackages = [ "cmd/glt" ];
            ldflags = [ "-s" "-w" "-X main.version=${version}" ];
            env.CGO_ENABLED = 0;
            meta = {
              description = "Fast terminal UI for GitLab merge requests and pipelines";
              mainProgram = "glt";
              license = pkgs.lib.licenses.mit;
            };
          };
          default = glt;
        };

        apps.default = flake-utils.lib.mkApp { drv = self.packages.${system}.glt; };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [ go gopls gotools golangci-lint delve ];
        };
      });
}
