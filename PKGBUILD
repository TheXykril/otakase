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
  'rofi: graphical selection menus'
  'ueberzugpp: image previews in the terminal'
  'ffmpeg: saving episodes with -download'
)
makedepends=('go' 'git')
# curd is what this program used to be called. Anyone who has it installed would
# otherwise end up with two copies of the same tool fighting over /usr/bin.
conflicts=('curd' 'curd-bin' 'curd-git')
replaces=('curd')
# Declared so a future package cannot quietly take the same paths.
provides=('otakase' 'otk' 'curd')
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
  # Muscle memory, and any script that still calls curd.
  ln -s otakase "$pkgdir/usr/bin/curd"
  install -Dm644 README.md "$pkgdir/usr/share/doc/$pkgname/README.md"
}
