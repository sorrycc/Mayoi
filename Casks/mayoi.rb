cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.16.0"
  sha256 arm:   "46127b817eeaa70692fbb6c7739c4fe34a6da7f9b69a198d17395b429e68c439",
         intel: "7db8eed989f284672f31ffc1c54e3f0308b2fe3e0dc3c3d2e1bef5448def6479"

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
