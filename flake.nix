{
  description = "Cartograph engine: one pinned toolchain, the binary, the container image, on x86_64 and aarch64";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      # Both architectures the releases target, plus macOS for laptops.
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forEachSystem = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = builtins.replaceStrings [ "\n" ] [ "" ] (builtins.readFile ./VERSION);
    in
    {
      packages = forEachSystem (pkgs:
        let
          lib = pkgs.lib;

          # The binary. Tests run in the build (buildGoModule's checkPhase),
          # so `nix build` is also the gate. The interface is embedded from
          # internal/spa/dist, a release artifact of cartograph-ui.
          cartograph = pkgs.buildGoModule {
            pname = "cartograph";
            inherit version;
            src = lib.cleanSource ./.;
            vendorHash = "sha256-CJmZABpIIRGrUFEBCPl08d9GNQfDOlTsFcQ4jMuk4NA=";
            subPackages = [ "cmd/cartograph" ];
            env.CGO_ENABLED = 0;
            ldflags = [ "-s" "-w" "-X main.version=${version}" ];
            meta = {
              description = "Cartograph: a vault of project, goal and KPI definitions, with its interface embedded";
              mainProgram = "cartograph";
              license = lib.licenses.asl20;
            };
          };

          # A container image is a Linux root filesystem: the binary, CA
          # certificates, a writable /vault owned by the runtime user, and
          # the binary's own readiness check. No shell, no browser.
          imageFor = { name, extra ? [ ], env ? [ ] }: pkgs.dockerTools.buildLayeredImage {
            inherit name;
            tag = version;
            contents = [ cartograph pkgs.cacert ] ++ extra;
            fakeRootCommands = ''
              mkdir -p ./vault ./tmp
              chown 65532:65532 ./vault
              chmod 1777 ./tmp
            '';
            config = {
              Entrypoint = [ "${cartograph}/bin/cartograph" ];
              Cmd = [ "serve" ];
              User = "65532:65532";
              Env = [ "CARTOGRAPH_VAULT=/vault" "CARTOGRAPH_LOG_FORMAT=json" "HOME=/tmp" ] ++ env;
              ExposedPorts = { "8080/tcp" = { }; };
              Volumes = { "/vault" = { }; };
              Healthcheck = {
                Test = [ "CMD" "${cartograph}/bin/cartograph" "ready" ];
                Interval = 10000000000;
                Timeout = 3000000000;
                StartPeriod = 5000000000;
              };
            };
          };
        in
        { inherit cartograph; default = cartograph; }
        // lib.optionalAttrs pkgs.stdenv.hostPlatform.isLinux {
          image = imageFor { name = "cartograph"; };
          # The same, plus Chromium for PDF printing.
          image-chromium = imageFor {
            name = "cartograph-chromium";
            extra = [ pkgs.chromium ];
            env = [ "CARTOGRAPH_CHROMIUM=${pkgs.chromium}/bin/chromium" ];
          };
        });

      devShells = forEachSystem (pkgs:
        let haveChromium = pkgs.stdenv.hostPlatform.isLinux; in {
          default = pkgs.mkShell {
            name = "cartograph-engine";
            packages = with pkgs; [
              go
              go-tools # staticcheck
              (python3.withPackages (ps: [ ps.pyyaml ])) # scripts/check-compat
              just
              bashInteractive
              coreutils
              diffutils
              gnugrep
              gnused
              curl
              git
            ] ++ pkgs.lib.optionals haveChromium [ chromium ];
            shellHook = ''
              ${pkgs.lib.optionalString haveChromium ''export CHROMIUM="${pkgs.chromium}/bin/chromium"''}
              export GOTOOLCHAIN=local
              export CARTOGRAPH_TOOLCHAIN=1
            '';
          };
        });

      apps = forEachSystem (pkgs:
        let cartograph = self.packages.${pkgs.stdenv.hostPlatform.system}.cartograph; in {
          default = { type = "app"; program = "${cartograph}/bin/cartograph"; meta.description = "Run the cartograph command line"; };
          cartograph = self.apps.${pkgs.stdenv.hostPlatform.system}.default;
        });

      checks = forEachSystem (pkgs: { cartograph = self.packages.${pkgs.stdenv.hostPlatform.system}.cartograph; });
      formatter = forEachSystem (pkgs: pkgs.nixpkgs-fmt);
    };
}
