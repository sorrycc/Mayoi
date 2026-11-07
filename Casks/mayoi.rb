cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.18.2"
  sha256 arm:   "74e44e521c1d9faa2f80246666e1d95a2c5c470c21851f1f14b99e5e343c89ce",
         intel: "482105fa851318ad381f3fd1b02c5792764db1cb582e8f9df599e5c80f31620f"

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
