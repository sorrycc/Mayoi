cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.21.2"
  sha256 arm:   "cae0ddc99fdc2e27060c53d1ad6e3602a95cbfcda4de54da64fd1cfc6888293a",
         intel: "06646d937e0864cd0a3ece31723590b9593ec61112925451b2f8bfe42e492c2c"

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
