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

## Remote compatibility manifest and mandatory startup update

The live manifest is stored in this repository at `config/provider-compatibility.json`, with its detached Ed25519 signature at `config/provider-compatibility.json.sig`.

- Every app launch downloads the current manifest and signature from the repository's `main` branch, then the `release/v1.0.8-candidate` branch as a secondary location.
- The app verifies the signature against the Ed25519 public key pinned in the native core. It then validates the schema, expiry, monotonically increasing version, HTTPS host allowlists, endpoint names, path templates, and built-in parser profile names.
- Native source and playback actions remain locked until a fresh manifest has been downloaded and verified. If GitHub is unreachable, the manifest is expired, or its signature is invalid, D-TV stays on the retry screen rather than silently using an unverified configuration.
- The manifest can switch only among parser strategies already compiled into D-TV. It cannot contain JavaScript, executable code, arbitrary redirects, credentials, device IDs, or API tokens.

### Updating the manifest

1. Edit `config/provider-compatibility.json` and increment `manifestVersion`. Keep `generatedAt` current and `expiresAt` within 90 days.
2. Push the JSON change to `main` or a `release/**` branch. The `Sign Provider Compatibility Manifest` workflow creates the matching `.sig` file.
3. The workflow requires the repository Actions secret `DTV_PROVIDER_COMPAT_SIGNING_PRIVATE_KEY`. Its value is the Base64-encoded 32-byte Ed25519 seed supplied to the repository owner. Never commit the private key or put it in the manifest.
4. Wait until the signature workflow succeeds before expecting clients to accept the new manifest. A JSON file whose signature is missing or stale is intentionally rejected.

The app does not use a stale cache to bypass the startup check. This is an intentional fail-closed policy because startup freshness is a user requirement.

## Red Fruit (Hongguo) route recovery

The Red Fruit playback adapter uses multiple built-in official API routes and remembers the last successful route for five minutes. The signed manifest controls the route order. On the response side, D-TV handles single-video and multi-video model shapes, `fallback_api`, `backup_url_list`, the current `spade_a` envelope, and encrypted media URLs that require `key_seed`.

The public THLRedGuo source also uses a private signing profile supplied through its own GitHub Actions secret. That secret is not present in the public repository and is not copied into D-TV. D-TV keeps its own native signing path and adapts the public API-route, response parsing, media-key, and fallback behavior. If the upstream signing protocol changes, treat that as a code-level compatibility update and add a fixture test; do not place signing material in the public JSON.

The goal is to correct a host, API path, or route order centrally without waiting for an app-store release, while keeping parser changes, signature verification, and device-specific signing logic inside the reviewed native code.
