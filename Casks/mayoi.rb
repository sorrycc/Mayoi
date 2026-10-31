cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.16.1"
  sha256 arm:   "fb0e0d5211d0b4f1a089995a0504a3e252c7bd5d5c0503850661cb4822012c93",
         intel: "cb484aa63a50c97857d2ce7d83fc455ecff711eb029a9e775767169fb21f09df"

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
