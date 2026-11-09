cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.19.0"
  sha256 arm:   "2c7c0428f50577c98bd1592e2768c9b26198e4464c03f0aad49e3e9fe4cea386",
         intel: "35e57d40238a985313e44f349ca9f3a55cc97283a17554f784b112ac5d5b6927"

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
