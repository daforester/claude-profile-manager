#!/bin/sh
# Declares the claude:// URL scheme in a packaged Claude Profile Manager.app,
# so macOS can hand sign-in links to it. `fyne package` has no option for
# CFBundleURLTypes, so it is added to Info.plist afterwards.
# Usage: scripts/macos-url-scheme.sh "Claude Profile Manager.app"
set -eu

app=${1:?usage: $0 path/to/App.app}
plist="$app/Contents/Info.plist"
pb=/usr/libexec/PlistBuddy

"$pb" -c "Delete :CFBundleURLTypes" "$plist" 2>/dev/null || true
"$pb" \
	-c "Add :CFBundleURLTypes array" \
	-c "Add :CFBundleURLTypes:0 dict" \
	-c "Add :CFBundleURLTypes:0:CFBundleURLName string Claude sign-in link" \
	-c "Add :CFBundleURLTypes:0:CFBundleTypeRole string Viewer" \
	-c "Add :CFBundleURLTypes:0:CFBundleURLSchemes array" \
	-c "Add :CFBundleURLTypes:0:CFBundleURLSchemes:0 string claude" \
	"$plist"
plutil -lint "$plist"
[ "$("$pb" -c "Print :CFBundleURLTypes:0:CFBundleURLSchemes:0" "$plist")" = claude ]
