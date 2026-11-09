"""Build the site's help pages and changelog from the repo.

  python3 .github/site_pages.py <site dir>

wiki/*.md become <site>/help/<Page>.html (Home is also help/index.html), with
wiki/_Sidebar.md as the menu, and CHANGELOG.md becomes
<site>/changelog/index.html with one section per version (#26.2.0). GitHub
Pages serves Page.html at /help/Page, so the wiki's own links between pages
work unchanged. Heading anchors follow GitHub's rule, the same one the
Discord bot uses when it links a wiki section.

Needs the markdown package (python3-markdown on Ubuntu).
"""

import html
import pathlib
import re
import sys

import markdown

ROOT = pathlib.Path(__file__).resolve().parent.parent
REPO = "https://github.com/TheXykril/otakase"


def anchor(text, sep="-"):
    """GitHub's heading anchor: lower case, spaces to dashes, other
    punctuation dropped (bot/wiki.go wikiAnchor)."""
    text = re.sub(r"<[^>]+>", "", html.unescape(text)).lower()
    return "".join(c if c.isalnum() or c in "-_" else sep if c == " " else "" for c in text)


def site_links(md):
    """Point links that still lead to the repository at the site."""
    md = re.sub(re.escape(REPO) + r"/wiki/([\w-]+)", r"/help/\1", md)
    md = re.sub(re.escape(REPO) + r"/wiki\b", "/help/", md)
    md = re.sub(re.escape(REPO) + r"(/releases\S*|#install|#readme)?(?=[)\s])", "/#download", md)
    return md


def render(md):
    conv = markdown.Markdown(extensions=["extra", "sane_lists", "toc"],
                             extension_configs={"toc": {"slugify": anchor}})
    return conv.convert(site_links(md))


PAGE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>{title} · Otakase</title>
<meta name="description" content="{description}">
<meta name="theme-color" content="#0e0d0c">
<link rel="icon" href="/img/favicon.svg" type="image/svg+xml">
<link rel="icon" href="/img/favicon-32.png" sizes="32x32" type="image/png">
<link rel="apple-touch-icon" href="/img/apple-touch-icon.png">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=Archivo+Black&family=JetBrains+Mono&display=swap">
<link rel="stylesheet" href="/docs.css?v=dev">
</head>
<body>
<header class="top"><a class="brand" href="/"><img src="/img/favicon.svg" alt="" width="28" height="28">Otakase</a>
<nav><a href="/help/"{help_on}>Help</a><a href="/changelog/"{log_on}>Changelog</a><a href="/#download">Download</a><a href="/discord/">Discord</a></nav></header>
<div class="layout">
{menu}<main class="doc">
{body}
</main>
</div>
<footer><span>お任せ · Otakase · Otakase does not host or upload any video.</span><nav><a href="/help/">Help</a><a href="/changelog/">Changelog</a><a href="/discord/">Discord</a></nav></footer>
</body>
</html>
"""


def page(title, description, body, menu="", section="help"):
    return PAGE.format(title=html.escape(title), description=html.escape(description, quote=True),
                       body=body, menu=menu,
                       help_on=' aria-current="page"' if section == "help" else "",
                       log_on=' aria-current="page"' if section == "changelog" else "")


def first_paragraph(md):
    for block in re.split(r"\n\s*\n", md):
        block = block.strip()
        if block and not block.startswith(("#", "|", "-", "*", "```", ">")):
            text = re.sub(r"\[([^\]]+)\]\([^)]*\)", r"\1", " ".join(block.split()))
            return re.sub(r"[*_`]", "", text)[:200]
    return ""


def build_help(site):
    wiki = ROOT / "wiki"
    out = site / "help"
    out.mkdir(parents=True, exist_ok=True)
    sidebar = wiki / "_Sidebar.md"
    menu = ""
    if sidebar.exists():
        menu = '<aside class="menu">\n' + render(sidebar.read_text(encoding="utf-8")) + "\n</aside>\n"
    pages = sorted(p for p in wiki.glob("*.md") if not p.name.startswith("_"))
    for src in pages:
        md = src.read_text(encoding="utf-8")
        m = re.search(r"^#\s+(.+)$", md, re.M)
        title = m.group(1).strip() if m else src.stem.replace("-", " ")
        if src.stem == "Home":
            title = "Help"
        html_page = page(title, first_paragraph(md) or "Otakase help", render(md), menu)
        (out / f"{src.stem}.html").write_text(html_page, encoding="utf-8")
        if src.stem == "Home":
            (out / "index.html").write_text(html_page, encoding="utf-8")
    return [p.stem for p in pages]


def build_changelog(site):
    md = (ROOT / "CHANGELOG.md").read_text(encoding="utf-8")
    # One section per version. "Unreleased" stays out: it is not out yet.
    parts = re.split(r"^(?=## )", md, flags=re.M)
    head, sections = parts[0], parts[1:]
    body = [render(head)]
    for sec in sections:
        heading, _, rest = sec.partition("\n")
        name = heading[3:].strip()
        if name.lower().startswith("unreleased"):
            continue
        version = name.split(" ")[0]
        body.append(f'<section id="{html.escape(version, quote=True)}">'
                    f'<h2><a href="#{html.escape(version, quote=True)}">{html.escape(name)}</a></h2>\n'
                    + render(rest) + "</section>")
    out = site / "changelog"
    out.mkdir(parents=True, exist_ok=True)
    (out / "index.html").write_text(
        page("Changelog", "What changed in each Otakase release.", "\n".join(body), section="changelog"),
        encoding="utf-8")
    return len(body) - 1


def main():
    site = pathlib.Path(sys.argv[1])
    pages = build_help(site)
    versions = build_changelog(site)
    print(f"help: {', '.join(pages)}; changelog: {versions} releases")


if __name__ == "__main__":
    main()
