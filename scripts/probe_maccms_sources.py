#!/usr/bin/env python3
"""Read-only live probe for public MacCMS-style video APIs.

This script checks catalogue, search, detail, episode groups, and a bounded
media-byte probe. It never authenticates, extracts app secrets, or downloads
full media files. URLs containing signed query parameters are not printed.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urljoin, urlsplit, urlunsplit

import requests


SOURCES = [
    {
        "name": "暴風資源",
        "api": "https://bfzyapi.com/api.php/provide/vod/",
        "referer": "https://bfzyapi.com/",
    },
    {
        "name": "鴨鴨資源",
        "api": "https://cj.yayazy.net/api.php/provide/vod/",
        "referer": "https://cj.yayazy.net/",
    },
    {
        "name": "極速資源",
        "api": "https://jszyapi.com/api.php/provide/vod/",
        "referer": "https://jszyapi.com/",
    },
    {
        "name": "360資源",
        "api": "https://360zy.com/api.php/provide/vod/",
        "referer": "https://360zy.com/",
    },
    {
        "name": "虎牙資源",
        "api": "https://huyazy.net/api.php/provide/vod/",
        "referer": "https://huyazy.net/",
    },
]

TIMEOUT = (8, 15)
USER_AGENT = (
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
    "AppleWebKit/537.36 (KHTML, like Gecko) "
    "Chrome/131.0.0.0 Safari/537.36 DTV-MacCMS-Probe/1.0"
)
ADULT_WORDS = ("倫理", "三級", "成人", "情色", "写真", "写真热舞", "擦边")
LIVE_WORDS = ("体育赛事", "篮球", "足球", "斯诺克", "直播")
DIRECT_SUFFIXES = (".m3u8", ".mp4")


def safe_url(raw: str) -> str:
    """Keep host/path for diagnostics but redact potentially signed query data."""
    try:
        parsed = urlsplit(raw)
        return urlunsplit((parsed.scheme, parsed.netloc, parsed.path, "", ""))
    except Exception:
        return "<invalid-url>"


def request_json(session: requests.Session, api: str, referer: str, params: dict) -> dict:
    response = session.get(
        api,
        params=params,
        headers={"Referer": referer, "Accept": "application/json,text/plain,*/*"},
        timeout=TIMEOUT,
    )
    response.raise_for_status()
    payload = response.json()
    if not isinstance(payload, dict):
        raise ValueError("API 回應不是 JSON 物件")
    code = str(payload.get("code", ""))
    if code not in ("1", "200"):
        raise ValueError(f"API 回傳 code={code or 'missing'}：{payload.get('msg', '')}")
    if not isinstance(payload.get("list"), list):
        raise ValueError("API 未回傳 list 陣列")
    return payload


def movie_like(row: dict) -> bool:
    category = str(row.get("type_name", ""))
    title = str(row.get("vod_name", ""))
    combined = category + " " + title
    if any(word in combined for word in ADULT_WORDS + LIVE_WORDS):
        return False
    return bool(str(row.get("vod_id", "")).strip() and title.strip())


def groups_from_detail(row: dict) -> list[dict]:
    raw_names = str(row.get("vod_play_from", "") or "").strip()
    raw_urls = str(row.get("vod_play_url", "") or "").strip()
    if not raw_names or not raw_urls:
        return []
    if "$$$" in raw_names:
        names = [part.strip() for part in raw_names.split("$$$") if part.strip()]
    elif "," in raw_names:
        names = [part.strip() for part in raw_names.split(",") if part.strip()]
    else:
        names = [raw_names]
    blocks = raw_urls.split("$$$")
    if len(blocks) == 1 and len(names) > 1 and "$$" in raw_urls:
        blocks = raw_urls.split("$$")
    count = min(len(names), len(blocks))
    groups = []
    for index in range(count):
        episodes = []
        for episode_index, item in enumerate(blocks[index].split("#"), 1):
            label, sep, address = item.strip().partition("$")
            address = address.strip()
            if not sep or not label.strip() or not address.startswith(("https://", "http://")):
                continue
            episodes.append({
                "title": label.strip(),
                "url": address,
                "index": episode_index,
            })
        if episodes:
            groups.append({"name": names[index], "episodes": episodes})
    groups.sort(
        key=lambda group: (
            0 if "m3u8" in group["name"].lower()
            else 1 if "mp4" in group["name"].lower()
            else 2
        )
    )
    return groups


def probe_segment(session: requests.Session, address: str, referer: str) -> tuple[bool, str]:
    try:
        response = session.get(
            address,
            headers={"Referer": referer, "Range": "bytes=0-1023"},
            timeout=TIMEOUT,
            stream=True,
            allow_redirects=True,
        )
        try:
            content_type = response.headers.get("Content-Type", "").lower()
            sample = response.raw.read(1024, decode_content=True)
            ok = (
                response.status_code in (200, 206)
                and bool(sample)
                and "text/html" not in content_type
                and "application/json" not in content_type
            )
            return ok, f"HTTP {response.status_code}, {content_type or 'unknown content type'}, {len(sample)} bytes"
        finally:
            response.close()
    except requests.RequestException as exc:
        return False, f"{type(exc).__name__}: {exc}"


def probe_hls(session: requests.Session, address: str, referer: str) -> tuple[bool, str]:
    try:
        response = session.get(
            address,
            headers={"Referer": referer, "Range": "bytes=0-262143"},
            timeout=TIMEOUT,
            stream=True,
            allow_redirects=True,
        )
        try:
            content_type = response.headers.get("Content-Type", "").lower()
            body = response.raw.read(262144, decode_content=True)
            text = body.decode("utf-8-sig", errors="replace")
            if response.status_code not in (200, 206):
                return False, f"播放清單 HTTP {response.status_code}"
            if "text/html" in content_type or "application/json" in content_type:
                return False, f"播放清單回傳非媒體內容：{content_type}"
            if not text.lstrip().startswith("#EXTM3U"):
                return False, "回應不是有效 M3U8"
            first_uri = ""
            lines = [line.strip() for line in text.splitlines()]
            for index, line in enumerate(lines):
                if line.startswith("#EXT-X-STREAM-INF:"):
                    for candidate in lines[index + 1:]:
                        if candidate and not candidate.startswith("#"):
                            first_uri = candidate
                            break
                    if first_uri:
                        break
            if not first_uri:
                first_uri = next((line for line in lines if line and not line.startswith("#")), "")
            if not first_uri:
                return False, "M3U8 沒有媒體分段"
            child = urljoin(response.url, first_uri)
        finally:
            response.close()

        if urlsplit(child).path.lower().endswith(".m3u8"):
            try:
                child_response = session.get(
                    child,
                    headers={"Referer": referer, "Range": "bytes=0-262143"},
                    timeout=TIMEOUT,
                    stream=True,
                    allow_redirects=True,
                )
                try:
                    child_type = child_response.headers.get("Content-Type", "").lower()
                    child_body = child_response.raw.read(262144, decode_content=True)
                    child_text = child_body.decode("utf-8-sig", errors="replace")
                    if child_response.status_code not in (200, 206) or not child_text.lstrip().startswith("#EXTM3U"):
                        return False, "子播放清單無效"
                    segment = next(
                        (line.strip() for line in child_text.splitlines()
                         if line.strip() and not line.strip().startswith("#")),
                        "",
                    )
                    if not segment:
                        return False, "子播放清單沒有媒體分段"
                    child = urljoin(child_response.url, segment)
                finally:
                    child_response.close()
            except requests.RequestException as exc:
                return False, f"子播放清單錯誤：{type(exc).__name__}: {exc}"

        ok, message = probe_segment(session, child, referer)
        if not ok:
            return False, "M3U8 正常，但首個分段探測失敗：" + message
        return True, "M3U8 與首個媒體分段均可讀取；" + message
    except requests.RequestException as exc:
        return False, f"{type(exc).__name__}: {exc}"


def probe_media(session: requests.Session, address: str, referer: str) -> tuple[bool, str]:
    path = urlsplit(address).path.lower()
    if path.endswith(".m3u8"):
        return probe_hls(session, address, referer)
    if path.endswith(".mp4"):
        ok, message = probe_segment(session, address, referer)
        if not ok:
            return False, message
        return True, "MP4 位址有回傳媒體位元組；" + message
    return False, "不是可直接辨識的 M3U8／MP4，可能需要來源專屬解析器"


def probe_source(source: dict) -> dict:
    result = {
        "name": source["name"],
        "api": source["api"],
        "catalog": False,
        "search": False,
        "detail": False,
        "episodes": False,
        "playback": False,
        "steps": [],
    }
    session = requests.Session()
    session.headers.update({"User-Agent": USER_AGENT})
    try:
        catalog = request_json(
            session, source["api"], source["referer"],
            {"ac": "list", "pg": "1", "at": "json"},
        )
        rows = catalog["list"]
        result["catalog"] = len(rows) > 0
        result["steps"].append({
            "step": "catalog",
            "ok": result["catalog"],
            "total": catalog.get("total"),
            "rows": len(rows),
            "classes": len(catalog.get("class", [])) if isinstance(catalog.get("class"), list) else 0,
            "message": "取得影片列表" if rows else "列表為空",
        })
        sample = next((row for row in rows if isinstance(row, dict) and movie_like(row)), None)
        if sample is None:
            raise ValueError("目錄沒有可供測試的非成人／非體育影片")
        title = str(sample.get("vod_name", "")).strip()
        video_id = str(sample.get("vod_id", "")).strip()
        result["sample_title"] = title
        result["sample_id"] = video_id

        search_payload = None
        search_error = None
        for query in (title, title[:4] if len(title) > 4 else title):
            try:
                search_payload = request_json(
                    session, source["api"], source["referer"],
                    {"ac": "list", "wd": query, "pg": "1", "at": "json"},
                )
                if search_payload["list"]:
                    break
            except Exception as exc:  # record endpoint compatibility, then try next mode
                search_error = str(exc)
                try:
                    search_payload = request_json(
                        session, source["api"], source["referer"],
                        {"ac": "videolist", "wd": query, "pg": "1", "at": "json"},
                    )
                    if search_payload["list"]:
                        break
                except Exception as fallback_exc:
                    search_error = str(fallback_exc)
                    search_payload = None
        search_rows = search_payload.get("list", []) if search_payload else []
        result["search"] = bool(search_rows)
        result["steps"].append({
            "step": "search",
            "ok": result["search"],
            "query": title,
            "results": len(search_rows),
            "message": "搜尋有結果" if search_rows else (search_error or "搜尋沒有結果"),
        })

        detail_payload = None
        detail_errors = []
        for action in ("detail", "videolist", "list"):
            try:
                detail_payload = request_json(
                    session, source["api"], source["referer"],
                    {"ac": action, "ids": video_id, "at": "json"},
                )
                if detail_payload["list"]:
                    break
                detail_payload = None
            except Exception as exc:
                detail_errors.append(str(exc))
        detail_rows = detail_payload.get("list", []) if detail_payload else []
        detail_row = detail_rows[0] if detail_rows and isinstance(detail_rows[0], dict) else None
        result["detail"] = bool(detail_row and str(detail_row.get("vod_id", "")).strip())
        result["steps"].append({
            "step": "detail",
            "ok": result["detail"],
            "message": "取得影片詳情" if result["detail"] else "; ".join(detail_errors) or "詳情沒有回傳影片資料",
        })
        if not result["detail"]:
            return result

        groups = groups_from_detail(detail_row)
        episode_total = sum(len(group["episodes"]) for group in groups)
        result["episodes"] = episode_total > 0
        result["play_groups"] = [
            {"name": group["name"], "episode_count": len(group["episodes"])}
            for group in groups
        ]
        result["episode_count"] = episode_total
        result["steps"].append({
            "step": "episodes",
            "ok": result["episodes"],
            "groups": len(groups),
            "episodes": episode_total,
            "message": "已取得分集與播放線路" if result["episodes"] else "詳情沒有可解析的分集播放網址",
        })
        if not result["episodes"]:
            return result

        candidates = []
        for group in groups:
            for episode in group["episodes"]:
                if urlsplit(episode["url"]).path.lower().endswith(DIRECT_SUFFIXES):
                    candidates.append((group["name"], episode))
        candidates.sort(key=lambda item: (
            0 if urlsplit(item[1]["url"]).path.lower().endswith(".m3u8") else 1
        ))
        if not candidates:
            result["steps"].append({
                "step": "playback",
                "ok": False,
                "message": "目前分集均為解析頁／非直連格式，尚不能證明可播放",
            })
            return result

        for group_name, episode in candidates[:3]:
            ok, message = probe_media(session, episode["url"], source["referer"])
            result["steps"].append({
                "step": "playback",
                "ok": ok,
                "line": group_name,
                "episode": episode["title"],
                "url": safe_url(episode["url"]),
                "message": message,
            })
            if ok:
                result["playback"] = True
                break
    except Exception as exc:
        result["steps"].append({
            "step": "error",
            "ok": False,
            "message": f"{type(exc).__name__}: {exc}",
        })
    finally:
        session.close()
    return result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", default="maccms-source-probe-report.json")
    args = parser.parse_args()

    report = {
        "checked_at": datetime.now(timezone.utc).isoformat(),
        "probe_type": "read-only public MacCMS API and bounded media-byte probe",
        "results": [probe_source(source) for source in SOURCES],
    }
    output = Path(args.output)
    output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False, indent=2))
    passing = sum(
        1 for item in report["results"]
        if item["catalog"] and item["search"] and item["detail"] and item["episodes"] and item["playback"]
    )
    print(f"\n來源完整通過數：{passing}/{len(report['results'])}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
