#!/usr/bin/env python3
"""Regenerate internal/icons/font/OtakaseSymbols.ttf.

The font otakase installs for its menu icons is Symbols Nerd Font Mono cut
down to the glyphs internal/icons/icons.go uses, and renamed so it never
stands in for a full Nerd Font. Run this after adding an icon there.

    pip install fonttools
    curl -LO https://github.com/ryanoasis/nerd-fonts/releases/latest/download/NerdFontsSymbolsOnly.zip
    unzip NerdFontsSymbolsOnly.zip SymbolsNerdFontMono-Regular.ttf
    python3 Build/icon-font.py SymbolsNerdFontMono-Regular.ttf
"""

import pathlib
import re
import sys

from fontTools import subset
from fontTools.ttLib import TTFont

ROOT = pathlib.Path(__file__).resolve().parent.parent
ICONS = ROOT / "internal" / "icons" / "icons.go"
OUTPUT = ROOT / "internal" / "icons" / "font" / "OtakaseSymbols.ttf"
FAMILY = "Otakase Symbols"


def codepoints():
    # The Icon constants, e.g. `Download Icon = 0xF01DA`.
    found = re.findall(r"Icon\s*=\s*0x([0-9A-Fa-f]+)", ICONS.read_text())
    if not found:
        sys.exit(f"no icon codepoints found in {ICONS}")
    return sorted({int(value, 16) for value in found})


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    source = sys.argv[1]
    wanted = codepoints()

    options = subset.Options()
    options.layout_features = []
    options.hinting = False
    options.desubroutinize = True
    options.name_IDs = ["*"]
    font = TTFont(source)
    subsetter = subset.Subsetter(options)
    subsetter.populate(unicodes=wanted)
    subsetter.subset(font)

    missing = [cp for cp in wanted if cp not in font.getBestCmap()]
    if missing:
        sys.exit("source font lacks: " + ", ".join(f"U+{cp:X}" for cp in missing))

    names = font["name"]
    for record in list(names.names):
        if record.nameID in (1, 16):
            names.setName(FAMILY, record.nameID, record.platformID, record.platEncID, record.langID)
        elif record.nameID == 4:
            names.setName(FAMILY, 4, record.platformID, record.platEncID, record.langID)
        elif record.nameID == 6:
            names.setName("OtakaseSymbols-Regular", 6, record.platformID, record.platEncID, record.langID)

    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    font.save(OUTPUT)
    print(f"wrote {OUTPUT.relative_to(ROOT)}: {len(wanted)} glyphs, {OUTPUT.stat().st_size} bytes")


if __name__ == "__main__":
    main()
