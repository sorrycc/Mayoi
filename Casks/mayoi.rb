cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.19.1"
  sha256 arm:   "81457c428ff3c59a1f18f20806fa02d9c760b7530c861bc150acd5b1c508ce34",
         intel: "34e4c75262a51cc4b0820e1e77011df31615579a38506b4e21fc7f3031b3f739"

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
