#!/bin/sh
# Builds the Linux packages from binaries already in stage/:
#   stage/claude-profile-manager  (GUI)   stage/cpm  (CLI)
# into dist/: .deb (Debian, Ubuntu, Mint, Pop!_OS, ...), .rpm (Fedora, RHEL
# and rebuilds, openSUSE, Mageia, ...), .pkg.tar.zst (Arch, Manjaro,
# EndeavourOS, ...) and an AppImage for everything else.
# Needs nfpm and, for the AppImage, appimagetool (both on PATH).
# Usage: packaging/linux/build-packages.sh VERSION RELEASE
set -eu

export PKG_VERSION=${1:?usage: $0 VERSION RELEASE}
export PKG_RELEASE=${2:?usage: $0 VERSION RELEASE}
id=io.github.claudeprofilemanager
mkdir -p dist

for p in deb rpm archlinux; do
	nfpm package -f packaging/nfpm.yaml -p "$p" -t dist/
done

# AppImage: the app plus its launcher and icon. Graphics and X11/Wayland
# libraries come from the host, as for any desktop app.
app=$(mktemp -d)/AppDir
install -Dm755 stage/claude-profile-manager "$app/usr/bin/claude-profile-manager"
install -Dm755 stage/cpm "$app/usr/bin/cpm"
install -Dm644 packaging/linux/$id.desktop "$app/usr/share/applications/$id.desktop"
install -Dm644 assets/icon.png "$app/usr/share/icons/hicolor/512x512/apps/$id.png"
cp packaging/linux/$id.desktop assets/icon.png "$app/"
mv "$app/icon.png" "$app/$id.png"
cat >"$app/AppRun" <<'RUN'
#!/bin/sh
# `cpm` as the first argument runs the console CLI; anything else goes to
# the GUI binary, which also accepts the CLI commands.
here=$(dirname "$(readlink -f "$0")")
if [ "${1:-}" = cpm ]; then
	shift
	exec "$here/usr/bin/cpm" "$@"
fi
exec "$here/usr/bin/claude-profile-manager" "$@"
RUN
chmod 755 "$app/AppRun"
# appimagetool is itself an AppImage; extracting avoids needing FUSE.
APPIMAGE_EXTRACT_AND_RUN=1 ARCH=x86_64 VERSION="$PKG_VERSION" \
	appimagetool --no-appstream "$app" "dist/Claude_Profile_Manager-$PKG_VERSION-x86_64.AppImage"
rm -rf "$(dirname "$app")"
