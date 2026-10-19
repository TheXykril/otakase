# Maintainer: TheXykril <48257623+TheXykril@users.noreply.github.com>
pkgname='otakase'
pkgver=26.7.0
pkgrel=1
pkgdesc="Watch anime in CLI with AniList/MyAnimeList tracking, Discord RPC, and intro/outro/filler/recap skipping"
arch=('x86_64' 'aarch64')
url="https://github.com/TheXykril/otakase"
license=('GPL3')
depends=('mpv')
optdepends=(
  'rofi: graphical selection menus, the Super+Shift+A keybinding, and opening otakase:// links without a terminal'
  # Without a terminal there is nowhere else for a message to go, so a launch
  # from a keybinding that fails is silent without this.
  'libnotify: progress and errors when launched without a terminal'
  'xdg-utils: opening the browser for AniList/MyAnimeList sign-in'
  'ffmpeg: saving episodes with -download'
)
makedepends=('go' 'git')
install='otakase.install'
source=("$pkgname-$pkgver.tar.gz::https://github.com/TheXykril/otakase/archive/refs/tags/v${pkgver}.tar.gz")
sha256sums=('SKIP')

prepare() {
  cd "$srcdir/otakase-$pkgver"
  # Fetch modules here so build() and check() can run offline.
  go mod download
}

build() {
  cd "$srcdir/otakase-$pkgver"
  export CGO_ENABLED=0
  export GOFLAGS="-trimpath -mod=readonly -modcacherw -buildvcs=false"
  go build -ldflags="-X main.version=${pkgver} -s -w" -o otakase ./cmd/otakase
}

check() {
  cd "$srcdir/otakase-$pkgver"
  # -short skips the live provider tests, which need network access.
  go test -short -mod=readonly ./...
}

package() {
  cd "$srcdir/otakase-$pkgver"
  install -Dm755 otakase "$pkgdir/usr/bin/otakase"
  # otk is the short form, for something typed several times a day.
  ln -s otakase "$pkgdir/usr/bin/otk"
  install -Dm644 README.md "$pkgdir/usr/share/doc/$pkgname/README.md"
  # The app-menu entry and its icon. otakase installs its own copies under
  # ~/.local/share only when these are missing.
  install -Dm644 internal/appicon/otakase.desktop "$pkgdir/usr/share/applications/otakase.desktop"
  # Opens otakase:// links in the rofi menus, without a terminal.
  install -Dm644 internal/appicon/otakase-link.desktop "$pkgdir/usr/share/applications/otakase-link.desktop"
  install -Dm644 internal/appicon/otakase.svg "$pkgdir/usr/share/icons/hicolor/scalable/apps/otakase.svg"
  for size in 16 24 32 48 64 128 256 512; do
    install -Dm644 "Build/app-icon/png/otakase-$size.png" \
      "$pkgdir/usr/share/icons/hicolor/${size}x${size}/apps/otakase.png"
  done
}
