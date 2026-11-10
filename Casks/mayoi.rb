cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.19.3"
  sha256 arm:   "69f2355dca0a839070eaa6c0e3ce249c59126e3fab9b8f363df0a22bbbdeebf4",
         intel: "6803450440d4ba4dff5b354c30262e95702cff22e1d2e86df3da3d7a73771751"

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
