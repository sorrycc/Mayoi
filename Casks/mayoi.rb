cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.22.4"
  sha256 arm:   "4269114a8098f7b9c437b32dd7f50ce99c56d10eba7c030e443813fb3683129a",
         intel: "bc47666abf3f7a0ecf7d9318a583adba1a194baa586850343bfc73ffc50eb442"

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
