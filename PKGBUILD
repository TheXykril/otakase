# Maintainer: TheXykril <pijus.skirmantas@gmail.com>
# Fork of Wraient/curd with provider resolution fixes.
pkgname='curd'
pkgver=2.3.0
pkgrel=1
pkgdesc="Watch anime in CLI with AniList tracking, Discord RPC, and intro/outro/filler/recap skipping (fork with provider fixes)"
arch=('x86_64' 'aarch64')
url="https://github.com/TheXykril/curd"
license=('GPL3')
depends=('mpv')
optdepends=(
  'rofi: graphical selection menus'
  'ueberzugpp: image previews in the terminal'
)
makedepends=('go' 'git')
conflicts=('curd-bin' 'curd-git')
source=("$pkgname-$pkgver.tar.gz::https://github.com/TheXykril/curd/archive/refs/tags/v${pkgver}.tar.gz")
sha256sums=('SKIP')

build() {
  cd "$srcdir/curd-$pkgver"
  export CGO_ENABLED=0
  export GOFLAGS="-trimpath -mod=vendor -buildvcs=false"
  go build -ldflags="-X main.version=${pkgver} -s -w" -o curd ./cmd/curd
}

check() {
  cd "$srcdir/curd-$pkgver"
  # -short skips the live provider tests, which need network access.
  go test -short -mod=vendor ./...
}

package() {
  cd "$srcdir/curd-$pkgver"
  install -Dm755 curd "$pkgdir/usr/bin/curd"
  install -Dm644 README.md "$pkgdir/usr/share/doc/$pkgname/README.md"
}
