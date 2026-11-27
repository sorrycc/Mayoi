cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.23.5"
  sha256 arm:   "167acc32fd12f6a0dfdabea38adfe2cc7338fd02499123f8fb1544514aa270d6",
         intel: "cfa597cb8a2ddb94c36088c4498276ea427b39350951d9043b950c5162c3d31c"

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
