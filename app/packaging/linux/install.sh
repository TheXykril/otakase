#!/bin/sh
# Puts the AppImage where it stays and hooks it into the desktop, for the
# current user only:
#
#   ~/.local/share/otakase-app/otakase.AppImage   the app itself
#   ~/.local/bin/otakase                          the terminal command
#   ~/.local/share/applications/otakase.desktop   app menus and launchers
#   ~/.local/share/icons/hicolor/.../otakase.*    the icon
#
# Started by AppRun:  otakase.AppImage --install | --uninstall
# AppRun also runs --refresh on every start of the installed copy, so a new
# version's menu entry and icon replace the old ones.
#
# The in-app updater replaces the installed AppImage in place, so the
# command and the menu entry keep working across updates.
set -eu

action=${1:-}
appdir=${APPDIR:-$(dirname "$(dirname "$(dirname "$(dirname "$(readlink -f "$0")")")")")}
data=${XDG_DATA_HOME:-$HOME/.local/share}
case $data in /*) ;; *) data=$HOME/.local/share ;; esac
home_dir=$data/otakase-app
image=$home_dir/otakase.AppImage
bin_dir=$HOME/.local/bin
link=$bin_dir/otakase
apps=$data/applications
entry=$apps/otakase.desktop
icons=$data/icons/hicolor
icon_sizes="128 256 512"

say() { [ "$action" = --refresh ] || printf '%s\n' "$*"; }

# Writes stdin to $1 when it differs; says whether it changed anything.
put() {
  tmp=$1.new.$$
  mkdir -p "$(dirname "$1")"
  cat > "$tmp"
  if [ -f "$1" ] && cmp -s "$tmp" "$1"; then
    rm -f "$tmp"
    return 1
  fi
  mv -f "$tmp" "$1"
}

desktop_entry() {
  cat <<EOF
[Desktop Entry]
Type=Application
Name=Otakase
GenericName=Video Player
Comment=Anime, movies and shows, with your list kept in sync
Exec="$image"
TryExec=$image
Icon=otakase
Terminal=false
Categories=AudioVideo;Video;Player;
Keywords=anime;anilist;myanimelist;trakt;movies;
StartupWMClass=com.xyverion.otakase
StartupNotify=true
Actions=launcher;
X-Otakase-AppImage=true

[Desktop Action launcher]
Name=Quick launcher
Exec="$image" --launcher
EOF
}

# Menu entry and icons. Returns 0 when something changed.
integrate() {
  changed=1
  for size in $icon_sizes; do
    png=$appdir/usr/share/icons/hicolor/${size}x${size}/apps/otakase.png
    [ -f "$png" ] || continue
    put "$icons/${size}x${size}/apps/otakase.png" < "$png" && changed=0
  done
  desktop_entry | put "$entry" && changed=0
  if [ ! -L "$link" ] || [ "$(readlink "$link")" != "$image" ]; then
    if [ -e "$link" ] && [ ! -L "$link" ]; then
      # Something else lives there (an old build copied by hand, say).
      # Keep it beside, renamed, instead of losing it.
      mv -f "$link" "$link.old"
      say "Moved the old $link to $link.old."
    fi
    mkdir -p "$bin_dir"
    ln -sfn "$image" "$link"
    changed=0
  fi
  return $changed
}

# Lets the desktop see a new or removed entry and icon. Best effort.
refresh_caches() {
  if command -v update-desktop-database > /dev/null 2>&1; then
    update-desktop-database -q "$apps" 2> /dev/null || true
  fi
  if command -v gtk-update-icon-cache > /dev/null 2>&1 && [ -f "$icons/index.theme" ]; then
    gtk-update-icon-cache -q -t "$icons" 2> /dev/null || true
  fi
  touch "$icons" 2> /dev/null || true
}

ours() {
  [ -f "$entry" ] && grep -q '^X-Otakase-AppImage=true' "$entry"
}

case $action in
  --install)
    src=${APPIMAGE:-}
    if [ -z "$src" ] || [ ! -f "$src" ]; then
      echo "Run this from the Otakase AppImage: ./otakase-app-linux-x86_64.AppImage --install" >&2
      exit 1
    fi
    mkdir -p "$home_dir"
    if [ "$(readlink -f "$src")" != "$(readlink -f "$image" 2> /dev/null || echo "$image")" ]; then
      cp -f "$src" "$image.new.$$"
      chmod 755 "$image.new.$$"
      mv -f "$image.new.$$" "$image"
    fi
    integrate || true
    refresh_caches
    say "Otakase is installed."
    say "  Open it from your app menu, or type: otakase"
    say "  Quick launcher: otakase --launcher"
    if [ "$(readlink -f "$src")" != "$(readlink -f "$image")" ]; then
      say "  You can delete $src now."
    fi
    case ":$PATH:" in
      *":$bin_dir:"*)
        found=$(command -v otakase 2> /dev/null || true)
        if [ -n "$found" ] && [ "$found" != "$link" ]; then
          say ""
          say "Note: 'otakase' runs $found first, which comes before $bin_dir in your PATH."
          say "Remove it to use this install: rm $found"
        fi
        ;;
      *)
        say ""
        say "Note: $bin_dir is not in your PATH yet, so the terminal will not find 'otakase'."
        say "Add this line to your shell's startup file (~/.bashrc, ~/.zshrc), then open a new terminal:"
        say "  export PATH=\"\$HOME/.local/bin:\$PATH\""
        ;;
    esac
    ;;
  --refresh)
    # Only the installed copy keeps the entry current, and only when it was
    # installed this way (removing the entry by hand sticks).
    [ -n "${APPIMAGE:-}" ] && [ -f "$image" ] || exit 0
    [ "$(readlink -f "$APPIMAGE")" = "$(readlink -f "$image")" ] && ours || exit 0
    if integrate; then refresh_caches; fi
    ;;
  --uninstall)
    if [ -L "$link" ] && [ "$(readlink "$link")" = "$image" ]; then
      rm -f "$link"
    fi
    if ours; then rm -f "$entry"; fi
    for size in $icon_sizes; do
      rm -f "$icons/${size}x${size}/apps/otakase.png"
    done
    rm -f "$image"
    rmdir "$home_dir" 2> /dev/null || true
    refresh_caches
    say "Otakase is removed from this computer. Your profiles, accounts and progress are kept."
    ;;
  *)
    echo "usage: otakase.AppImage --install | --uninstall" >&2
    exit 2
    ;;
esac
