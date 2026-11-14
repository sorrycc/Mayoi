cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.21.0"
  sha256 arm:   "d95647500215ea30f09588f149f0623a5008c6b94ab4a714dc16ff106288bc39",
         intel: "5782047a8bd096331aaa49135cdcdc07577c8fcb3c11598c623a89e1edd8ca7e"

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
