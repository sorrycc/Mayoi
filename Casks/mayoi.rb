cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.19.2"
  sha256 arm:   "85e3bc89c45e76612f1c220c347a0e46b1566384178d0f1bb0d3900040bdbbc2",
         intel: "9fe7690c8aa03cad8661d82dad346f6b764eb5e6d1dcbcb31fe3f717488bfbcc"

  url "https://github.com/sorrycc/Mayoi/releases/download/v#{version}/mayoi-#{version}-#{arch}.dmg"
  name "Mayoi"
  desc "Small, fast code editor"
  homepage "https://github.com/sorrycc/Mayoi"

  auto_updates true
  depends_on macos: :ventura

  app "Mayoi.app"
  command_wrapper "mayoi", content: <<~SH
    #!/bin/sh
    # Managed by Homebrew; use brew uninstall --cask mayoi to remove it.
    app=#{(appdir/"Mayoi.app/Contents/MacOS/Mayoi").to_s.shellescape}
    [ -x "$app" ] || { echo "mayoi: Mayoi.app is not found" >&2; exit 1; }
    MAYOI_CLI=1 exec "$app" "$@"
  SH
end
