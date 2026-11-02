cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.17.0"
  sha256 arm:   "e0e19cc03a4d25fa18d18ad82728713ec78b4ab48db16aab1fb0cf0bbbf676f3",
         intel: "bfec5c0515fe754953a81156539a95202756fb2df89931a427c72f958a6e4e49"

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
