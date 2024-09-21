{
  description = "telegraf-plugin-gadgetbridge";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-parts,
    }@inputs:

    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      perSystem =
        { self', pkgs, ... }:
        {
          devShells.default = pkgs.mkShell {
            packages = with pkgs; [
              self'.formatter
              go_1_22
              gopls
              gotools
            ];
          };

          packages.default = pkgs.buildGoModule {
            src = ./.;
            pname = "telegraf-plugin-gadgetbridge";
            version = self.rev or "unknown";
            vendorHash = "sha256-NkrGq86DqS1BQZMDRXsNxqwmHN16EcB8/ciYS2+281A=";
            meta = {
              description = "Telegraf plugin that ingests data from Gadgetbridge's auto-export file and sends it to Telegraf.";
              mainProgram = "telegraf-plugin-gadgetbridge";
            };
          };

          formatter = pkgs.nixfmt-rfc-style;
        };
    };
}
