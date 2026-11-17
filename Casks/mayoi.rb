cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.22.2"
  sha256 arm:   "3cb7a336c713e9e458ea95482a043b3b41d17bed3de1a9df04ce698845c158c1",
         intel: "6ea542cbd6c7e1e7b95a986e829287b127e8b408e4fc53f29aba2d042728c809"

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
