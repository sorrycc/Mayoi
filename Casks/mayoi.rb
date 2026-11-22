cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.23.2"
  sha256 arm:   "4445da5a745dfc78a9f29186fa8e575692381b7350aab473a9fb57274934c97b",
         intel: "f0d0b522497789031bc44f43a0c4a63e0954cbe3a19df84cf32566c7c9641d48"

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
