# Maintainer: TheXykril <48257623+TheXykril@users.noreply.github.com>
# Continuation of Wraient/curd, rebuilt around provider resolution and theming.
pkgname='otakase'
pkgver=1.0.0
pkgrel=1
pkgdesc="Watch anime in CLI with AniList/MyAnimeList tracking, Discord RPC, and intro/outro/filler/recap skipping"
arch=('x86_64' 'aarch64')
url="https://github.com/TheXykril/otakase"
license=('GPL3')
depends=('mpv')
optdepends=(
  'rofi: graphical selection menus, and the Super+Shift+A keybinding'
  # Without a terminal there is nowhere else for a message to go, so a launch
  # from a keybinding that fails is silent without this.
  'libnotify: progress and errors when launched without a terminal'
  'xdg-utils: opening the browser for AniList/MyAnimeList sign-in'
  'ffmpeg: saving episodes with -download'
)
makedepends=('go' 'git')
# curd is what this program used to be called. Installing this removes that
# package rather than sitting beside it; the curd command is not carried over.
conflicts=('curd' 'curd-bin' 'curd-git')
replaces=('curd')
install='otakase.install'
source=("$pkgname-$pkgver.tar.gz::https://github.com/TheXykril/otakase/archive/refs/tags/v${pkgver}.tar.gz")
sha256sums=('SKIP')

build() {
  cd "$srcdir/otakase-$pkgver"
  export CGO_ENABLED=0
  export GOFLAGS="-trimpath -mod=vendor -buildvcs=false"
  go build -ldflags="-X main.version=${pkgver} -s -w" -o otakase ./cmd/otakase
}

check() {
  cd "$srcdir/otakase-$pkgver"
  # -short skips the live provider tests, which need network access.
  go test -short -mod=vendor ./...
}

package() {
  cd "$srcdir/otakase-$pkgver"
  install -Dm755 otakase "$pkgdir/usr/bin/otakase"
  # otk is the short form, for something typed several times a day.
  ln -s otakase "$pkgdir/usr/bin/otk"
  install -Dm644 README.md "$pkgdir/usr/share/doc/$pkgname/README.md"
}
