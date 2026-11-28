cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.23.6"
  sha256 arm:   "81f8a5ac74df350fbed9d24c2bfb4a35f289888bf22fb1f3447628ffa69aae1f",
         intel: "ae92b2e4b6eee4fb8944a546046a4c8a854c42fae89c4604ad460f2cea29524a"

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
