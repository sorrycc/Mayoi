cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.21.1"
  sha256 arm:   "6fdf54268f98dd354b5e64a7ad6ae4a3d7b2e9d8244d11948c257b48c8cdb2c5",
         intel: "211089db3dd346f34db761e38c662ccf9f4207f6cdd29ab3d0af592eab28e075"

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
