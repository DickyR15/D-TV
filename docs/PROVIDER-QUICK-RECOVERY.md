# Provider Quick Recovery Playbook

This guide is for source-site changes that break catalogues, posters, episode lists, or playback.

## Goal

Repair one provider without changing the shared player or destabilizing unrelated providers. Prefer a small compatibility patch plus fixture tests over a broad rewrite.

## First response: classify the failure

Collect a D-TV diagnostic export and record:

- D-TV version/build, OS, device model, provider ID, work ID, and the exact action that failed.
- Request stage: catalogue, detail, image, episode list, URL resolution, playlist preparation, or player open.
- HTTP status, final host/path (redact signed query strings), response content type, elapsed time, and parser counts.
- Which compatibility attempt was used and why it failed.

Never include cookies, authorization headers, API keys, complete signed media URLs, or personal account data in shared logs.

| Symptom | Likely change | First place to inspect |
|---|---|---|
| HTTP 404 / 410 | Site route or host changed | Provider base URL and path builder |
| HTTP 401 / 403 | Header, session, or anti-bot policy changed | User-Agent, Referer, request sequence; never hard-code a user's session |
| HTTP 200 but zero cards | HTML structure, JSON shape, or lazy-image attribute changed | HTML/JSON parser and saved fixture |
| Detail works but no episodes | Playback API parameters or response schema changed | Detail parser and episode-line parser |
| URL resolves but player fails | CDN, media format, playlist, or request headers changed | Provider media preparation and route fallback |
| Poster missing or blank | Lazy image, anti-hotlink, invalid image, or identity mismatch | Cover candidates, image validation, and exact-title lookup |

## Safe repair sequence

1. **Capture a fixture.** Save a minimal HTML/JSON sample with identifiers and signed values removed. Preserve the fields and structure that caused the failure.
2. **Add a new compatibility path.** Keep the previous parser/route as a fallback until the new path has passed tests. Do not replace a working path just because the site has changed for one region or device.
3. **Normalize at the provider boundary.** Convert every response into the existing Drama, Chapter, and providerMedia models. The shared player should not need to know a provider's HTML or API details.
4. **Validate destinations.** Enforce HTTPS and provider/CDN host allowlists. Reject userinfo, unexpected ports, and redirects to untrusted hosts. Never blindly follow a URL supplied by page content.
5. **Keep all usable routes.** Choose one preferred route, order remaining routes by known quality and source order, and try them automatically after a real playback failure. Distinct audio languages remain separate user choices.
6. **Add tests before removing old behavior.** Cover the new response shape, old response shape, malformed data, bad hosts, missing images, and fallback ordering.
7. **Measure on each platform.** Compare URL resolution time, player setup time, first-frame time, and fallback count on Windows, Android, and iOS. Do not infer playback success from URL parsing alone.

## iKanBot example: many movie lines

Some iKanBot movie records return dozens of CDN rows for the same film. Those rows are routes, not episodes. D-TV should expose one movie choice with a preferred route and automatic backups. Only explicit language variants such as 國語版 and 日語版 should remain separate choices.

The regression test TestIkanbotMovieThirtyOneSourceRowsUseOnePrimaryAndThirtyFallbacks protects this behavior.

## Poster recovery

Poster resolution follows this order:

1. Original provider artwork and provider-supplied image candidates.
2. Verified title aliases and exact source-specific artwork.
3. Independent catalogue lookup (Douban, Bangumi, AniList for anime, TVmaze for series).
4. A neutral placeholder if identity or image validation fails.

Every automatic external match must be checked against normalized title aliases, work type, release year when available, and uniqueness. Never choose the first fuzzy search result. Keep a list of alternative image URLs when there are verified mirrors so a slow or blocked host does not hold the poster slot indefinitely.

## Long-term fast hotfix path

For future releases, keep provider compatibility data separate from parser code where possible:

- A small versioned manifest should hold allowed hosts, endpoint paths, response mode, and parser profile names.
- The app should ship a known-good embedded manifest and may fetch a newer signed/HTTPS manifest.
- Validate every remote manifest against a strict schema, version ceiling, host allowlist, and rollback policy.
- The manifest may switch among parser profiles already implemented in the app; it must never provide executable code, arbitrary JavaScript, or unrestricted URLs.
- When the official site changes beyond existing parser profiles, make a narrow code update and add a fixture so the same break does not recur.

This allows a route or selector change to be corrected centrally without waiting for a full app-store release, while preserving a safe embedded fallback when the remote manifest is unavailable.
