cask "mayoi" do
  arch arm: "arm64", intel: "amd64"

  version "0.11.11"
  sha256 arm:   "b04e4ecf7c7986b6bca94245953c202c4eb7fd82552908e437815ac8e4389f5a",
         intel: "48bd74c2aecf92674cc0ff223f156d318e97c62d3b4679255814e60990a7581d"

  url "https://github.com/sorrycc/Mayoi/releases/download/v#{version}/Mayoi.#{version}.#{arch}.dmg"
  name "Mayoi"
  desc "Small, fast code editor"
  homepage "https://github.com/sorrycc/Mayoi"

  auto_updates true
  depends_on macos: :ventura

  app "Mayoi.app"
  command_wrapper "mayoi", content: <<~SH
    #!/bin/sh
    # Managed by Homebrew; use brew uninstall --cask mayoi to remove it.
    app=#{(appdir/"Mayoi.app").to_s.shellescape}
    if [ ! -x "$app/Contents/MacOS/Mayoi" ]; then
      echo "mayoi: Mayoi.app is not found" >&2
      exit 1
    fi
    version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist") || exit 1
    if [ "$version" = "0.11.11" ]; then
      exec /usr/bin/open -a "$app" -- "$@"
    fi
    MAYOI_CLI=1 exec "$app/Contents/MacOS/Mayoi" "$@"
  SH
end
