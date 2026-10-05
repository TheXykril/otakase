#!/usr/bin/env python3
"""Post a release announcement to Discord.

Reads the version's section from CHANGELOG.md and sends a card (header image,
the bold lead of each changelog entry, buttons to the release) to the webhook
in DISCORD_RELEASE_WEBHOOK, pinging DISCORD_RELEASE_ROLE if set.

    post_release.py render VERSION OUT.html   # header page to screenshot
    post_release.py post VERSION HEADER.png   # send the post
"""
import datetime
import html
import json
import os
import re
import sys
import urllib.request
import uuid

REPO = "https://github.com/TheXykril/otakase"
SITE = "https://otakase.xyverion.com/"
SHU = 0xD2492F
MAX_ITEMS = 6
HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))


def changelog_section(version):
    text = open(os.path.join(ROOT, "CHANGELOG.md"), encoding="utf-8").read()
    m = re.search(rf"^## {re.escape(version)}\b(.*?)$\n(.*?)(?=^## |\Z)", text, re.M | re.S)
    if not m:
        sys.exit(f"CHANGELOG.md has no section for {version}")
    date = re.search(r"\d{4}-\d{2}-\d{2}", m.group(1))
    groups, current = [], None
    for line in m.group(2).splitlines():
        if line.startswith("### "):
            current = (line[4:].strip(), [])
            groups.append(current)
        elif line.startswith("- ") and current:
            current[1].append(line[2:])
        elif line.startswith("  ") and current and current[1]:
            current[1][-1] += " " + line.strip()
    return (date.group(0) if date else None), groups


def lead(entry):
    """The bold lead of an entry, or its first sentence."""
    m = re.match(r"\*\*(.+?)\*\*", entry)
    s = m.group(1) if m else re.split(r"(?<=\.)\s", entry)[0]
    s = s.strip().rstrip(".")
    return s if len(s) <= 110 else s[:107].rstrip() + "…"


def subtitle(date):
    if not date:
        return "a new release"
    d = datetime.date.fromisoformat(date)
    return f"released {d.strftime('%B')} {d.day}, {d.year}"


def render(version, out):
    date, _ = changelog_section(version)
    page = open(os.path.join(HERE, "release-header.html"), encoding="utf-8").read()
    page = page.replace("{{VERSION}}", html.escape(version)).replace("{{SUBTITLE}}", html.escape(subtitle(date)))
    open(out, "w", encoding="utf-8").write(page)


def payload(version, date, groups, role, has_image):
    text = lambda s: {"type": 10, "content": s}
    sep = {"type": 14, "divider": True, "spacing": 1}
    head = f"## otakase {version} is out"
    meta = []
    if role:
        meta.append(f"<@&{role}>")
    if date:
        ts = int(datetime.datetime.fromisoformat(date).replace(tzinfo=datetime.timezone.utc).timestamp())
        meta.append(f"<t:{ts}:D>")
    if meta:
        head += "\n" + "  ·  ".join(meta)
    card = [text(head), sep]
    for title, entries in groups:
        if not entries:
            continue
        lines = [f"- {lead(e)}" for e in entries[:MAX_ITEMS]]
        if len(entries) > MAX_ITEMS:
            lines.append(f"- *and {len(entries) - MAX_ITEMS} more*")
        card.append(text(f"### {title}\n" + "\n".join(lines)))
    card += [sep, text("-# Update with `otakase -u`")]
    tag = f"v{version}"
    comps = []
    if has_image:
        comps.append({"type": 12, "items": [{"media": {"url": "attachment://header.png"}}]})
    comps.append({"type": 17, "accent_color": SHU, "components": card})
    comps.append({"type": 1, "components": [
        {"type": 2, "style": 5, "label": "Download", "url": f"{REPO}/releases/tag/{tag}"},
        {"type": 2, "style": 5, "label": "Full changelog", "url": f"{REPO}/blob/{tag}/CHANGELOG.md"},
        {"type": 2, "style": 5, "label": "Website", "url": SITE},
    ]})
    body = {"flags": 1 << 15, "components": comps,
            "allowed_mentions": {"roles": [role] if role else []}}
    if has_image:
        body["attachments"] = [{"id": 0, "filename": "header.png"}]
    return body


def post(version, header):
    hook = os.environ.get("DISCORD_RELEASE_WEBHOOK", "").strip()
    if not hook:
        print("DISCORD_RELEASE_WEBHOOK is not set; nothing to post.")
        return
    hook = re.sub(r"/github/?$", "", hook)
    role = os.environ.get("DISCORD_RELEASE_ROLE", "").strip()
    date, groups = changelog_section(version)
    has_image = bool(header) and os.path.exists(header)
    body = json.dumps(payload(version, date, groups, role, has_image)).encode()

    boundary = uuid.uuid4().hex
    parts = [f"--{boundary}\r\nContent-Disposition: form-data; name=\"payload_json\"\r\n"
             f"Content-Type: application/json\r\n\r\n".encode() + body + b"\r\n"]
    if has_image:
        parts.append(f"--{boundary}\r\nContent-Disposition: form-data; name=\"files[0]\"; filename=\"header.png\"\r\n"
                     f"Content-Type: image/png\r\n\r\n".encode() + open(header, "rb").read() + b"\r\n")
    data = b"".join(parts) + f"--{boundary}--\r\n".encode()

    req = urllib.request.Request(f"{hook}?wait=true&with_components=true", data=data, method="POST", headers={
        "Content-Type": f"multipart/form-data; boundary={boundary}",
        "User-Agent": "otakase-release (https://github.com/TheXykril/otakase, 1.0)",
    })
    with urllib.request.urlopen(req) as r:
        print("Posted:", json.load(r)["id"])


if __name__ == "__main__":
    if len(sys.argv) != 4 or sys.argv[1] not in ("render", "post"):
        sys.exit(__doc__)
    version = sys.argv[2].lstrip("v")
    (render if sys.argv[1] == "render" else post)(version, sys.argv[3])
