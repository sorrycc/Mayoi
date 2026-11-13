cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.20.1"
  sha256 arm:   "80460c55995226810a038c3f20ef2f5d870dac1ffadad1e5f0f175fb7d25fdca",
         intel: "c1cbfb36b005871ab4d446eb606f1f77d0e9d003c633c1bf318b33fd95de38b0"

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
