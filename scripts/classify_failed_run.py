import argparse
import re
from pathlib import Path

TRANSIENT_PATTERNS = [
    r"connection reset",
    r"connection refused",
    r"connection timed out",
    r"operation timed out",
    r"temporary failure in name resolution",
    r"could not resolve host",
    r"failed to download",
    r"unexpected eof",
    r"tls handshake timeout",
    r"i/o timeout",
    r"network is unreachable",
    r"remote end closed connection",
    r"server returned (?:5\d\d)",
    r"http (?:429|500|502|503|504)",
    r"rate limit exceeded",
    r"resource temporarily unavailable",
    r"transient failure persisted",
    r"download.*(?:failed|timed out)",
]
DETERMINISTIC_MARKERS = [
    "[retry] deterministic build/test failure detected",
    "error: extra argument",
    "undefined symbols for architecture",
    "the method .* isn't defined",
    "could not compile",
    "build failed",
    "failed to compile",
    "test failed",
]


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Classify a failed cross-platform run before automatic retry."
    )
    parser.add_argument("log_file", type=Path)
    args = parser.parse_args()

    try:
        log = args.log_file.read_text(encoding="utf-8", errors="replace")
    except OSError as error:
        print(f"Unable to read failed workflow log: {error}")
        print("retry=false")
        return 0

    lowered = log.lower()
    deterministic = any(
        re.search(pattern, lowered, flags=re.IGNORECASE | re.MULTILINE)
        for pattern in DETERMINISTIC_MARKERS
    )
    transient = any(
        re.search(pattern, lowered, flags=re.IGNORECASE | re.MULTILINE)
        for pattern in TRANSIENT_PATTERNS
    )

    retry = transient and not deterministic
    if retry:
        print("Classified as a transient infrastructure failure; failed jobs may be retried.")
    elif deterministic:
        print("Classified as a deterministic source/build failure; automatic retry is skipped.")
    else:
        print("No recognised transient failure signature; automatic retry is skipped.")

    output = "true" if retry else "false"
    output_path = Path(__import__("os").environ.get("GITHUB_OUTPUT", ""))
    if output_path.is_file():
        with output_path.open("a", encoding="utf-8") as stream:
            stream.write(f"retry={output}\n")
    else:
        print(f"retry={output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
