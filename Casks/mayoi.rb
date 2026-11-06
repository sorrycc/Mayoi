cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.18.1"
  sha256 arm:   "f49891bc38325863095107066269b9cde0514f5a096a8d5e75e7197f45540541",
         intel: "5abb23e8c14dba20ba3054676f2b420f98a6d9c4552f55769cd8e5fddd49250f"

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
