cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.16.2"
  sha256 arm:   "b24998523a958083602921a1a1eeef0b42b3446f8b6d411c7feefcc289f8e1e2",
         intel: "a8d03a4f207994bd000c4dc6aeb15340193ffa81e491cff59f9a5eaa8cb33ed3"

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
