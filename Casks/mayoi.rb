cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.23.1"
  sha256 arm:   "c29edafe1e9ded3605f0140d6c8a1f609de538b7d1e769ef2f67e36466c14953",
         intel: "b2df00840249f467f43f95e9881a8f49d7caaf06cba0f2d2d2b498df79e4e7c3"

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
