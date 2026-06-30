{
  description = "shopservatory - monitors second-hand shopping sites for new items and notifies you";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    treefmt-nix = {
      url = "github:numtide/treefmt-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    git-hooks-nix = {
      url = "github:cachix/git-hooks.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    inputs:
    let
      pname = "shopservatory";
      version = "0.4.0";

      mkShopservatory =
        pkgs:
        pkgs.buildGoModule {
          inherit pname version;
          src = inputs.self;
          vendorHash = "sha256-PrmpcU+wy5+1FskcLNONXwbH5u3B/omKdOMK5TZ5d5k=";
          subPackages = [ "cmd/shopservatory" ];
          meta = {
            description = "Monitors second-hand shopping sites for new items and notifies you";
            mainProgram = "shopservatory";
            license = pkgs.lib.licenses.mit;
            platforms = pkgs.lib.platforms.linux;
          };
        };
    in
    inputs.flake-parts.lib.mkFlake { inherit inputs; } {
      imports = [
        inputs.treefmt-nix.flakeModule
        inputs.git-hooks-nix.flakeModule
      ];

      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];

      perSystem =
        { config
        , self'
        , pkgs
        , ...
        }:
        {
          treefmt = {
            programs = {
              nixpkgs-fmt.enable = true;
              gofmt.enable = true;
              deadnix.enable = true;
              statix.enable = true;
            };
          };

          pre-commit.settings.hooks =
            let
              pureGo = name: cmd: {
                enable = true;
                pass_filenames = false;
                entry = pkgs.lib.mkForce (
                  toString (
                    pkgs.writeShellScript "shopservatory-${name}" ''
                      export CGO_ENABLED=0
                      exec ${pkgs.go}/bin/go ${cmd}
                    ''
                  )
                );
              };
            in
            {
              treefmt.enable = true;
              gotest = pureGo "gotest" "test ./...";
              govet = pureGo "govet" "vet ./...";
            };

          packages = rec {
            shopservatory = mkShopservatory pkgs;
            default = shopservatory;
          };

          devShells.default = pkgs.mkShell {
            inputsFrom = [ self'.packages.default ];
            nativeBuildInputs = with pkgs; [
              go
              gopls
              gotools
              go-tools
              golangci-lint
              chromium
              flaresolverr
            ];
            SHOPSERVATORY_CHROMIUM = pkgs.lib.getExe pkgs.chromium;
            CGO_ENABLED = "0";
            shellHook = config.pre-commit.installationScript;
          };
        };

      flake =
        let
          inherit (inputs.nixpkgs) lib;
        in
        {
          overlays.default = final: _prev: {
            shopservatory = mkShopservatory final;
          };

          nixosModules = rec {
            shopservatory = { pkgs, ... }: {
              imports = [ ./nix/module.nix ];

              options.services.shopservatory.package = lib.mkOption {
                type = lib.types.package;
                description = "The shopservatory package to use.";
                default = inputs.self.packages.${pkgs.stdenv.hostPlatform.system}.shopservatory;
              };
            };
            default = shopservatory;
          };
        };
    };
}
