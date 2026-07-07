{ lib, buildGoModule, apple-sdk ? null }:

buildGoModule {
  pname = "aloud";
  version = "0-unstable";
  src = ./.;

  # aloud has an Objective-C media-keys shim (mediakeys_darwin.m) -> needs CGO
  # + the macOS SDK frameworks. macOS-only; only wired up when apple-sdk is
  # passed in (see flake.nix).
  env.CGO_ENABLED = "1";
  buildInputs = lib.optionals (apple-sdk != null) [ apple-sdk ];

  # vendorHash can't be known before the first build. Leave lib.fakeHash, run
  # `nix build .#aloud`, and paste the "got: sha256-..." value it prints.
  vendorHash = "sha256-Tne/JjcvGLeokGkMPmdxeRRmIyriFHaWab2JGfIzpkI=";

  meta = {
    description = "Text-to-speech reader / player with media-key control";
    homepage = "https://github.com/zachpmanson/aloud";
    mainProgram = "aloud";
  };
}
