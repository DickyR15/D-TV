#!/usr/bin/env python3
"""Publish the matching CHANGELOG.md section into GitHub Releases v1.0.1-v1.0.6."""

import json
import os
import re
import urllib.error
import urllib.request
from pathlib import Path

REPO = "DickyR15/D-TV"
VERSIONS = ("1.0.1", "1.0.2", "1.0.3", "1.0.4", "1.0.5", "1.0.6")
API = "https://api.github.com"
TOKEN = os.environ.get("GITHUB_TOKEN")
if not TOKEN:
    raise SystemExit("GITHUB_TOKEN is required.")

HEADERS = {
    "Accept": "application/vnd.github+json",
    "Authorization": "Bearer " + TOKEN,
    "X-GitHub-Api-Version": "2022-11-28",
    "User-Agent": "DTV-release-notes-sync",
}


def api(method, url, payload=None):
    headers = dict(HEADERS)
    headers["Content-Type"] = "application/vnd.github+json"
    req = urllib.request.Request(url, data=payload, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=60) as response:
            raw = response.read()
            return json.loads(raw.decode("utf-8")) if raw else {}
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError("{} {} failed: HTTP {} {}".format(method, url, exc.code, detail)) from exc


def changelog_section(text, version):
    lines = text.splitlines()
    heading_index = None
    for index, line in enumerate(lines):
        if re.match(r"^##\s+" + re.escape(version) + r"(?:（|\s|\()", line):
            heading_index = index
            break
    if heading_index is None:
        raise RuntimeError("CHANGELOG.md has no section for " + version)

    end = heading_index + 1
    while end < len(lines) and not lines[end].startswith("## "):
        end += 1

    section = "\n".join(lines[heading_index + 1:end]).strip()
    if not section:
        raise RuntimeError("CHANGELOG section is empty for " + version)

    # The release title is already displayed by GitHub; keep the section's
    # original headings, wording, bullets, and caveats exactly as authored.
    return section


def merge_release_body(existing, version, section):
    marker = "### 本次更新"
    block = marker + "\n\n" + section
    body = existing or ""

    if marker in body:
        pattern = re.compile(
            r"### 本次更新\s+.*?(?=\n### |\Z)",
            re.DOTALL,
        )
        updated, count = pattern.subn(block, body, count=1)
        if count != 1:
            raise RuntimeError("Could not safely replace the update section for " + version)
        return updated.rstrip() + "\n"

    # Add the full changelog section after the release's introductory
    # sentence, but before download assets and checksum instructions.
    download_heading = "\n### 下載檔案"
    if download_heading in body:
        before, after = body.split(download_heading, 1)
        return before.rstrip() + "\n\n" + block + "\n\n### 下載檔案" + after

    # Fallback for the oldest release's legacy heading layout.
    first_line, separator, remainder = body.partition("\n")
    if not separator:
        raise RuntimeError("Unexpected release body format for " + version)
    return first_line + "\n\n" + block + "\n\n" + remainder.lstrip()


def main():
    changelog = Path("CHANGELOG.md").read_text(encoding="utf-8")
    for version in VERSIONS:
        tag = "v" + version
        release = api(
            "GET",
            API + "/repos/" + REPO + "/releases/tags/" + tag,
        )
        section = changelog_section(changelog, version)
        updated_body = merge_release_body(release.get("body") or "", version, section)
        if updated_body == (release.get("body") or ""):
            print(tag + ": release notes already match CHANGELOG.md.")
            continue
        api(
            "PATCH",
            API + "/repos/" + REPO + "/releases/" + str(release["id"]),
            json.dumps({"body": updated_body}, ensure_ascii=False).encode("utf-8"),
        )
        print(tag + ": published CHANGELOG.md update notes.")
    print("Finished syncing update notes for D TV v1.0.1-v1.0.6.")


if __name__ == "__main__":
    main()
