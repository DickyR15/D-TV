#!/usr/bin/env python3
"""Normalize SHA-256 order for D TV releases v1.0.1 through v1.0.6."""

import hashlib
import json
import os
import re
import urllib.error
import urllib.parse
import urllib.request

OWNER_REPO = "DickyR15/D-TV"
VERSIONS = ("1.0.1", "1.0.2", "1.0.3", "1.0.4", "1.0.5", "1.0.6")
API = "https://api.github.com"
TOKEN = os.environ.get("GITHUB_TOKEN")
if not TOKEN:
    raise SystemExit("GITHUB_TOKEN is required.")

BASE_HEADERS = {
    "Accept": "application/vnd.github+json",
    "Authorization": "Bearer " + TOKEN,
    "X-GitHub-Api-Version": "2022-11-28",
    "User-Agent": "DTV-release-normalizer",
}


def request(method, url, data=None, content_type="application/vnd.github+json"):
    headers = dict(BASE_HEADERS)
    headers["Content-Type"] = content_type
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=60) as response:
            raw = response.read()
            return json.loads(raw.decode("utf-8")) if raw else {}
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError("{} {} failed: HTTP {} {}".format(method, url, exc.code, detail)) from exc


def normalize(version):
    tag = "v" + version
    release_url = API + "/repos/" + OWNER_REPO + "/releases/tags/" + tag
    release = request("GET", release_url)
    assets = release.get("assets", [])
    by_name = {asset["name"]: asset for asset in assets}

    # This order matches GitHub's alphabetical Assets display.
    filenames = [
        "DTV-" + version + "-Android.apk",
        "DTV-" + version + "-iOS.ipa",
        "DTV-" + version + "-macOS.dmg",
        "DTV-" + version + "-Windows-Portable.zip",
    ]
    lines = []
    for filename in filenames:
        asset = by_name.get(filename)
        if not asset:
            raise RuntimeError(tag + " is missing " + filename)
        digest = (asset.get("digest") or "").lower()
        if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
            raise RuntimeError(tag + " has no valid SHA-256 digest for " + filename)
        lines.append(digest.split(":", 1)[1] + "  " + filename)

    checksums = "\n".join(lines) + "\n"
    checksum_bytes = checksums.encode("utf-8")
    checksum_digest = hashlib.sha256(checksum_bytes).hexdigest()
    old_sums = by_name.get("SHA256SUMS.txt")

    # Avoid needless replacement when the existing asset is already correct.
    if old_sums and (old_sums.get("digest") or "").lower() == "sha256:" + checksum_digest:
        print(tag + ": SHA256SUMS.txt already has the canonical order.")
    else:
        if old_sums:
            delete_url = API + "/repos/" + OWNER_REPO + "/releases/assets/" + str(old_sums["id"])
            request("DELETE", delete_url)
            print(tag + ": removed the previous SHA256SUMS.txt.")

        upload_url = (
            "https://uploads.github.com/repos/" + OWNER_REPO + "/releases/"
            + str(release["id"]) + "/assets?"
            + urllib.parse.urlencode({"name": "SHA256SUMS.txt"})
        )
        uploaded = request("POST", upload_url, checksum_bytes, "application/octet-stream")
        if uploaded.get("name") != "SHA256SUMS.txt":
            raise RuntimeError(tag + ": SHA256SUMS.txt upload returned an unexpected result.")
        print(tag + ": uploaded the canonical SHA256SUMS.txt.")

    marker = "### SHA-256 檔案完整性驗證"
    body = release.get("body") or ""
    if marker not in body:
        raise RuntimeError(tag + ": checksum section not found; refusing to rewrite release notes.")

    fence = chr(96) * 3
    replacement = (
        marker + "\n\n"
        "以下為本次發布附件的 SHA-256 校驗碼，順序與下方 Assets 檔名排序一致。\n"
        "下載後可使用 SHA256SUMS.txt 進行比對。\n\n"
        + fence + "text\n" + checksums.rstrip() + "\n" + fence + "\n"
    )
    pattern = re.compile(
        r"### SHA-256 檔案完整性驗證\s+.*?(?=\n(?:此版本|完整更新內容))",
        re.DOTALL,
    )
    updated_body, count = pattern.subn(replacement.rstrip(), body, count=1)
    if count != 1:
        raise RuntimeError(tag + ": could not safely locate the checksum block.")

    if updated_body != body:
        request(
            "PATCH",
            API + "/repos/" + OWNER_REPO + "/releases/" + str(release["id"]),
            json.dumps({"body": updated_body}, ensure_ascii=False).encode("utf-8"),
        )
        print(tag + ": updated the release notes.")
    else:
        print(tag + ": release notes are already normalized.")


if __name__ == "__main__":
    for item in VERSIONS:
        normalize(item)
    print("Finished normalizing D TV v1.0.1-v1.0.6.")
