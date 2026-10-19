#!/usr/bin/env python3
"""Write the changelog block that goes above GitHub's generated release notes.

GitHub's notes list merged pull requests. Commits pushed straight to main
don't show up there, so this lists them, sorted by their subject prefix,
with the author's GitHub account where one can be found.

    release_notes.py OUT.md

Reads OTAKASE_VERSION, GITHUB_REPOSITORY and GITHUB_TOKEN from the
environment. Run it in a checkout with full history and tags.
"""
import json
import os
import re
import subprocess
import sys
import urllib.request

API = "https://api.github.com"

SECTIONS = [
    # heading, subject pattern; the first match wins, the last catches the rest
    ("### 🚀 Features", r"(feat|add)"),
    ("### 🐛 Bug Fixes", r"(fix|bug|hotfix)"),
    ("### 🔧 Maintenance & Improvements", r"(chore|clean|update|refactor|docs|style|test|ver)"),
    ("### 📝 Other Changes", r""),
]


def git(*args):
    return subprocess.run(["git", *args], check=True, capture_output=True,
                          text=True, encoding="utf-8").stdout


def api_get(path, token):
    req = urllib.request.Request(API + path, headers={
        "Authorization": f"Bearer {token}",
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
    })
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.load(resp)


def previous_tag(current):
    for tag in git("tag", "-l", "v*", "--sort=-version:refname").split():
        if tag != current:
            return tag
    return None


def log_range(prev):
    if prev:
        return f"{prev}..HEAD"
    roots = git("rev-list", "--max-parents=0", "HEAD").split()
    return f"{roots[0]}..HEAD" if roots else "HEAD"


def has_pull_request(repo, sha, token):
    # Those are already in GitHub's own notes. If the lookup fails, list the
    # commit rather than drop it.
    try:
        pulls = api_get(f"/repos/{repo}/commits/{sha}/pulls", token)
    except Exception:
        return False
    return any(pulls or [])


def author_of(repo, sha, name, token):
    try:
        login = (api_get(f"/repos/{repo}/commits/{sha}", token).get("author") or {}).get("login")
    except Exception:
        # The git name can be an account's old name, so say so in the log.
        print(f"::warning::Could not resolve the GitHub account for {sha}; using the commit's author name.")
        login = None
    return f"[@{login}](https://github.com/{login})" if login else name


def main():
    out = sys.argv[1]
    version = os.environ["OTAKASE_VERSION"].strip()
    repo = os.environ["GITHUB_REPOSITORY"]
    token = os.environ.get("GITHUB_TOKEN", "")
    tag = f"v{version}"

    prev = previous_tag(tag)
    raw = git("log", "--no-merges", "--pretty=format:%H%x09%s%x09%an", log_range(prev))

    entries = {heading: [] for heading, _ in SECTIONS}
    contributors = set()
    for line in raw.splitlines():
        parts = line.split("\t", 2)
        if len(parts) < 3:
            continue
        sha, subject, name = (p.strip() for p in parts)
        if not sha or not subject or re.match(r"\s*release:", subject, re.I):
            continue
        if has_pull_request(repo, sha, token):
            continue

        who = author_of(repo, sha, name, token)
        contributors.add(who)
        entry = f"- {subject} ([{sha[:7]}](https://github.com/{repo}/commit/{sha})) by {who}"
        heading = next(h for h, pat in SECTIONS if re.match(pat, subject, re.I))
        entries[heading].append(entry)

    # Read from the tag, so an old release keeps the banner it shipped with.
    banner = f"![Otakase](https://raw.githubusercontent.com/{repo}/{tag}/site/img/release-banner.png)"
    lines = [banner]

    # A release made only of pull requests is covered by GitHub's notes.
    if contributors:
        lines += ["", "## 📝 Changelog", ""]
        for heading, items in entries.items():
            if items:
                lines += [heading, *items, ""]
        lines += ["## 🤝 Contributors", "- " + ", ".join(sorted(contributors, key=str.casefold))]
        if prev:
            lines += ["", "---",
                      f"_Diff range: [{prev}..{tag}](https://github.com/{repo}/compare/{prev}...{tag})_"]

    with open(out, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")


if __name__ == "__main__":
    main()
