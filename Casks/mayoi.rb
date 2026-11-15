cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.22.0"
  sha256 arm:   "209a56cb8d61850dc92dd3977a03d0781cb9e1c34a1966d381e856817d7a45bd",
         intel: "2b134416651ea6818eec15183e2d27c6546f37d4f058536ecf65eb3b3d2f2fb1"

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
