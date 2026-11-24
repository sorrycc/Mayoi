cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.23.3"
  sha256 arm:   "311633083bcc9fa182381f4d679abf860a72e4b1b268df4b6d8b14090f0281af",
         intel: "c7f6d8cc77c9b91d3caabf588136e31001b802b4de1bf2599df0be9232d59d05"

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
