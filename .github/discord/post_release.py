#!/usr/bin/env python3
"""Post a release or dev build announcement to Discord.

Posts are sent by the Otakase bot (DISCORD_BOT_TOKEN), so they carry its
profile.

A release card reads the version's section from CHANGELOG.md: a header image,
the bold lead of each changelog entry and buttons to the release. It goes to
the DISCORD_RELEASE_CHANNEL channel, pinging DISCORD_RELEASE_ROLE if set, and
is published to servers following that channel when it is an announcement
channel.

A dev build card lists the feat/fix commits on main since the last release
commit, with buttons to the dev pre-release. It goes to DISCORD_DEV_CHANNEL,
pinging DISCORD_DEV_ROLE if set.

A channel is an id, or a name looked up in the DISCORD_GUILD server: either
the full name or the part after a "・" prefix, so "stable-builds" finds
"版・stable-builds" and keeps working when the prefix changes.

    post_release.py render VERSION OUT.html       # header page to screenshot
    post_release.py post VERSION HEADER.png       # send the release post
    post_release.py render-dev VERSION OUT.html
    post_release.py post-dev VERSION HEADER.png   # run in a checkout with history
"""
import datetime
import html
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request
import uuid

REPO = "https://github.com/TheXykril/otakase"
SITE = "https://otakase.xyverion.com/"
SHU = 0xD2492F
API = "https://discord.com/api/v10"
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


def render(version, out, dev=False):
    if dev:
        kana, sub = "試作", "test build · may be broken"
    else:
        kana, sub = "新版", subtitle(changelog_section(version)[0])
    page = open(os.path.join(HERE, "release-header.html"), encoding="utf-8").read()
    for k, v in (("{{KANA}}", kana), ("{{VERSION}}", version), ("{{SUBTITLE}}", sub)):
        page = page.replace(k, html.escape(v))
    open(out, "w", encoding="utf-8").write(page)


def dev_changes():
    """feat/fix commit subjects on HEAD since the last release commit."""
    try:
        log = subprocess.run(["git", "log", "--no-merges", "--format=%h%x09%s", "-n", "200"],
                             cwd=ROOT, capture_output=True, text=True, check=True).stdout
    except (OSError, subprocess.CalledProcessError):
        return []
    out = []
    for line in log.splitlines():
        sha, _, subject = line.partition("\t")
        if subject.startswith("release:"):
            break
        m = re.match(r"(feat|fix)(\([^)]*\))?!?:\s*(.+)", subject)
        if m:
            text = re.sub(r"\s*\(#\d+\)$", "", m.group(3))
            out.append((m.group(1), text[:1].upper() + text[1:], sha))
    return out


def text(s):
    return {"type": 10, "content": s}


SEP = {"type": 14, "divider": True, "spacing": 1}


def message(card, buttons, role, has_image):
    comps = []
    if has_image:
        comps.append({"type": 12, "items": [{"media": {"url": "attachment://header.png"}}]})
    comps.append({"type": 17, "accent_color": SHU, "components": card})
    comps.append({"type": 1, "components": [{"type": 2, "style": 5, "label": l, "url": u} for l, u in buttons]})
    body = {"flags": 1 << 15, "components": comps,
            "allowed_mentions": {"roles": [role] if role else []}}
    if has_image:
        body["attachments"] = [{"id": 0, "filename": "header.png"}]
    return body


def dev_payload(version, changes, role, has_image):
    head = f"## Test build {version}"
    if role:
        head += f"\n<@&{role}>"
    card = [text(head), SEP]
    groups = (("New", "feat"), ("Fixed", "fix"))
    for title, kind in groups:
        items = [c for c in changes if c[0] == kind]
        if items:
            lines = [f"- {t} ([`{h}`]({REPO}/commit/{h}))" for _, t, h in items[:8]]
            if len(items) > 8:
                lines.append(f"- *and {len(items) - 8} more*")
            card.append(text(f"### {title}\n" + "\n".join(lines)))
    if not changes:
        card.append(text("Internal changes only."))
    card += [SEP, text("-# Can be broken and is replaced by the next test build. "
                       "Set `DevBuilds=true` in the config to get it with `otakase -u`. "
                       "Report problems in the support forum.")]
    return message(card, [("Download", f"{REPO}/releases/tag/dev"),
                          ("Changes on main", f"{REPO}/commits/main")], role, has_image)


def payload(version, date, groups, role, has_image):
    head = f"## otakase {version} is out"
    meta = []
    if role:
        meta.append(f"<@&{role}>")
    if date:
        ts = int(datetime.datetime.fromisoformat(date).replace(tzinfo=datetime.timezone.utc).timestamp())
        meta.append(f"<t:{ts}:D>")
    if meta:
        head += "\n" + "  ·  ".join(meta)
    card = [text(head), SEP]
    for title, entries in groups:
        if not entries:
            continue
        lines = [f"- {lead(e)}" for e in entries[:MAX_ITEMS]]
        if len(entries) > MAX_ITEMS:
            lines.append(f"- *and {len(entries) - MAX_ITEMS} more*")
        card.append(text(f"### {title}\n" + "\n".join(lines)))
    card += [SEP, text("-# Update with `otakase -u`")]
    tag = f"v{version}"
    return message(card, [("Download", f"{REPO}/releases/tag/{tag}"),
                          ("Full changelog", f"{REPO}/blob/{tag}/CHANGELOG.md"),
                          ("Website", SITE)], role, has_image)


def request(method, path, token, data=None, content_type=None):
    headers = {"Authorization": f"Bot {token}",
               "User-Agent": "otakase-release (https://github.com/TheXykril/otakase, 1.0)"}
    if content_type:
        headers["Content-Type"] = content_type
    req = urllib.request.Request(API + path, data=data, method=method, headers=headers)
    with urllib.request.urlopen(req) as r:
        return json.load(r)


def match_channel(channels, wanted):
    """Returns the id of the text channel called wanted, with or without a
    "・" prefix."""
    for c in channels:
        if c.get("type") in (0, 5) and wanted in (c["name"], c["name"].split("・")[-1]):
            return c["id"]
    return None


def resolve_channel(channel, token):
    if channel.isdigit():
        return channel
    guild = os.environ.get("DISCORD_GUILD", "").strip()
    if not guild:
        sys.exit(f"channel {channel!r} is a name, so DISCORD_GUILD must be set")
    found = match_channel(request("GET", f"/guilds/{guild}/channels", token), channel)
    if not found:
        sys.exit(f"no text channel named {channel!r} in the server")
    return found


def post(version, header, dev=False):
    token = os.environ.get("DISCORD_BOT_TOKEN", "").strip()
    if not token:
        print("DISCORD_BOT_TOKEN is not set; nothing to post.")
        return
    prefix = "DISCORD_DEV" if dev else "DISCORD_RELEASE"
    channel = os.environ.get(f"{prefix}_CHANNEL", "").strip()
    if not channel:
        sys.exit(f"{prefix}_CHANNEL is not set")
    channel = resolve_channel(channel, token)
    role = os.environ.get(f"{prefix}_ROLE", "").strip()
    has_image = bool(header) and os.path.exists(header)
    if dev:
        body = dev_payload(version, dev_changes(), role, has_image)
    else:
        date, groups = changelog_section(version)
        body = payload(version, date, groups, role, has_image)
    body = json.dumps(body).encode()

    boundary = uuid.uuid4().hex
    parts = [f"--{boundary}\r\nContent-Disposition: form-data; name=\"payload_json\"\r\n"
             f"Content-Type: application/json\r\n\r\n".encode() + body + b"\r\n"]
    if has_image:
        parts.append(f"--{boundary}\r\nContent-Disposition: form-data; name=\"files[0]\"; filename=\"header.png\"\r\n"
                     f"Content-Type: image/png\r\n\r\n".encode() + open(header, "rb").read() + b"\r\n")
    data = b"".join(parts) + f"--{boundary}--\r\n".encode()

    msg = request("POST", f"/channels/{channel}/messages", token, data,
                  f"multipart/form-data; boundary={boundary}")
    print("Posted:", msg["id"])
    if request("GET", f"/channels/{channel}", token).get("type") == 5:
        try:
            request("POST", f"/channels/{channel}/messages/{msg['id']}/crosspost", token)
            print("Published to following servers.")
        except urllib.error.HTTPError as e:
            print(f"Could not publish: {e}")


if __name__ == "__main__":
    modes = {"render": render, "post": post, "render-dev": render, "post-dev": post}
    if len(sys.argv) != 4 or sys.argv[1] not in modes:
        sys.exit(__doc__)
    version = sys.argv[2].lstrip("v")
    modes[sys.argv[1]](version, sys.argv[3], dev=sys.argv[1].endswith("-dev"))
