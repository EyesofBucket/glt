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
        # the native package rebuilt for another OS/arch with Go's own
        # cross-compiler; the binary ends up at $out/bin/glt(.exe)
        crossBuild = goos: goarch: self.packages.${system}.glt.overrideAttrs (old: {
          pname = "glt-${goos}-${goarch}";
          env = old.env // { GOOS = goos; GOARCH = goarch; };
          doCheck = false; # the tests can't run on the build machine
          postInstall = ''
            if [ -d $out/bin/${goos}_${goarch} ]; then
              mv $out/bin/${goos}_${goarch}/* $out/bin/
              rmdir $out/bin/${goos}_${goarch}
            fi
          '';
        });
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

          # Windows builds (cross-compiled; no C toolchain needed since CGO is
          # off): nix build .#glt-windows / .#glt-windows-arm64
          glt-windows = crossBuild "windows" "amd64";
          glt-windows-arm64 = crossBuild "windows" "arm64";
        };

        apps.default = flake-utils.lib.mkApp { drv = self.packages.${system}.glt; };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [ go gopls gotools golangci-lint delve goreleaser ];
        };
      });
}
