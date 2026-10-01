{
  description = "Cartograph engine: one pinned toolchain, the binary, the container image, on x86_64 and aarch64";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  # A Rust toolchain with the wasm32-wasip1 standard library, which
  # nixpkgs does not ship. It builds the CRDT module (crdt/) and nothing
  # else; the development shell does not carry it.
  inputs.rust-overlay.url = "github:oxalica/rust-overlay";
  inputs.rust-overlay.inputs.nixpkgs.follows = "nixpkgs";

  outputs = { self, nixpkgs, rust-overlay }:
    let
      # Both architectures the releases target, plus macOS for laptops.
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forEachSystem = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = builtins.replaceStrings [ "\n" ] [ "" ] (builtins.readFile ./VERSION);
      trim = f: builtins.replaceStrings [ "\n" " " ] [ "" "" ] (builtins.readFile f);
    in
    {
      packages = forEachSystem (pkgs:
        let
          lib = pkgs.lib;

          # The web build the binary embeds: the cartograph-ui release
          # UI_VERSION names, pinned by UI_SHA256. scripts/fetch-ui reads
          # the same two files, so `just` and `nix build` embed the same
          # bytes.
          ui = pkgs.fetchurl {
            url = "https://github.com/ProjectCartograph/cartograph-ui/releases/download/${trim ./UI_VERSION}/dist.tar.gz";
            sha256 = trim ./UI_SHA256;
          };

          # The Automerge module the engine embeds (docs/adr/0007): the
          # crate in crdt/, for wasm32-wasip1, with every dependency taken
          # from Cargo.lock and the build directory remapped out of the
          # output, so two builds give the same bytes. `just generate`
          # copies it to internal/crdt/automerge and `just drift` refuses
          # a committed module that differs.
          rustWasm = (rust-overlay.lib.mkRustBin { } pkgs).stable."1.98.1".minimal.override {
            targets = [ "wasm32-wasip1" ];
          };
          rustPlatformWasm = pkgs.makeRustPlatform { cargo = rustWasm; rustc = rustWasm; };
          automerge-wasm = pkgs.stdenv.mkDerivation {
            pname = "automerge-wasm";
            version = "0.1.0";
            src = lib.fileset.toSource {
              root = ./crdt;
              fileset = lib.fileset.unions [ ./crdt/Cargo.toml ./crdt/Cargo.lock ./crdt/src ];
            };
            cargoDeps = rustPlatformWasm.importCargoLock { lockFile = ./crdt/Cargo.lock; };
            nativeBuildInputs = [ rustWasm rustPlatformWasm.cargoSetupHook ];
            buildPhase = ''
              runHook preBuild
              top=$(realpath "$NIX_BUILD_TOP")
              export RUSTFLAGS="--remap-path-prefix=$top=/build --remap-path-prefix=$NIX_BUILD_TOP=/build --remap-path-prefix=$cargoDeps=/cargo"
              export SOURCE_DATE_EPOCH=0
              cargo build --offline --frozen --release --target wasm32-wasip1
              runHook postBuild
            '';
            installPhase = ''
              runHook preInstall
              install -Dm444 target/wasm32-wasip1/release/cartograph_crdt.wasm $out/automerge.wasm
              runHook postInstall
            '';
            dontFixup = true;
            meta.license = lib.licenses.asl20;
          };

          # The binary. Tests run in the build (buildGoModule's checkPhase),
          # so `nix build` is also the gate.
          cartograph = pkgs.buildGoModule {
            pname = "cartograph";
            inherit version;
            src = lib.cleanSource ./.;
            vendorHash = "sha256-lElEGFTvYb6skF5rr5CxkGH34SeMNdXJnXCWKM9z+Ms=";
            subPackages = [ "cmd/cartograph" ];
            env.CGO_ENABLED = 0;
            ldflags = [ "-s" "-w" "-X main.version=${version}" ];
            preBuild = ''
              rm -rf internal/spa/dist && mkdir -p internal/spa/dist
              tar -xzf ${ui} -C internal/spa/dist
            '';
            # subPackages narrows the check phase to cmd/cartograph, which
            # has no tests of its own; the gate is every package.
            checkPhase = ''
              runHook preCheck
              go test -count=1 ./...
              runHook postCheck
            '';
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
        { inherit cartograph automerge-wasm; default = cartograph; }
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
              postgresql # `just test-postgres` starts a throwaway server
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
