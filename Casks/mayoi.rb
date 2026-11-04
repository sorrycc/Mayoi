cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.18.0"
  sha256 arm:   "100dbf6a4ea5ed3fae2bab7be99f325ecd5f4e4f11417d605365931de16144c3",
         intel: "98b9fcbf193717c3f28bfe87c02ea2f5b2d6056630ef5b074ce56ff2e517ab67"

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
