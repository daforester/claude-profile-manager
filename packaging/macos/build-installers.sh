#!/bin/sh
# Builds the macOS installers from a packaged app and the cpm CLI:
#   dist/claude-profile-manager-macos.dmg  drag-to-Applications disk image
#   dist/claude-profile-manager-macos.pkg  installer: the app into
#                                          /Applications, cpm into /usr/local/bin
# Usage: packaging/macos/build-installers.sh APP CPM VERSION
# (APP is "Claude Profile Manager.app", already with its claude:// scheme.)
# Neither is signed or notarised: the first open needs right-click > Open,
# or System Settings > Privacy & Security > Open Anyway.
set -eu

app=${1:?usage: $0 APP CPM VERSION}
cpm=${2:?usage: $0 APP CPM VERSION}
version=${3:?usage: $0 APP CPM VERSION}
id=io.github.claudeprofilemanager
work=$(mktemp -d)
mkdir -p dist

# Disk image: the app next to a link to /Applications.
mkdir "$work/dmg"
ditto "$app" "$work/dmg/$(basename "$app")"
ln -s /Applications "$work/dmg/Applications"
hdiutil create -volname "Claude Profile Manager" -srcfolder "$work/dmg" \
	-fs HFS+ -format UDZO -ov dist/claude-profile-manager-macos.dmg

# Installer package. The app component is marked non-relocatable, so an
# upgrade always lands in /Applications rather than wherever macOS last
# saw a copy of the app.
mkdir -p "$work/app" "$work/cli/usr/local/bin"
ditto "$app" "$work/app/$(basename "$app")"
pkgbuild --analyze --root "$work/app" "$work/app.plist"
plutil -replace 0.BundleIsRelocatable -bool NO "$work/app.plist"
pkgbuild --root "$work/app" --component-plist "$work/app.plist" \
	--identifier "$id" --version "$version" --install-location /Applications "$work/app.pkg"
install -m 755 "$cpm" "$work/cli/usr/local/bin/cpm"
pkgbuild --root "$work/cli" --identifier "$id.cpm" --version "$version" \
	--install-location / "$work/cli.pkg"
productbuild --package "$work/app.pkg" --package "$work/cli.pkg" \
	dist/claude-profile-manager-macos.pkg

rm -rf "$work"
