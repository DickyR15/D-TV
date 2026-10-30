import argparse
import re
import subprocess
import sys
import time

TRANSIENT_PATTERNS = [
    r"connection reset",
    r"connection refused",
    r"connection timed out",
    r"operation timed out",
    r"temporary failure",
    r"could not resolve host",
    r"failed to download",
    r"unexpected eof",
    r"tls handshake timeout",
    r"i/o timeout",
    r"network is unreachable",
    r"remote end closed connection",
    r"server returned (?:5\\d\\d)",
    r"http (?:429|500|502|503|504)",
    r"rate limit",
    r"resource temporarily unavailable",
    r"download.*(?:failed|timed out)",
]

def main():
    parser = argparse.ArgumentParser(
        description="Retry a build command only for recognised transient infrastructure failures."
    )
    parser.add_argument("--attempts", type=int, default=3)
    parser.add_argument("--delay", type=int, default=8)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    options = parser.parse_args()
    command = options.command[1:] if options.command[:1] == ["--"] else options.command
    if not command:
        parser.error("請在 -- 後指定要執行的命令。")
    if options.attempts < 1 or options.delay < 0:
        parser.error("--attempts 必須大於 0，--delay 不可小於 0。")

    for attempt in range(1, options.attempts + 1):
        print(
            f"[retry] attempt {attempt}/{options.attempts}: {' '.join(command)}",
            flush=True,
        )
        completed = subprocess.run(
            command,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            errors="replace",
        )
        output = completed.stdout or ""
        if output:
            sys.stdout.write(output)
            sys.stdout.flush()
        if completed.returncode == 0:
            print(f"[retry] command succeeded on attempt {attempt}.", flush=True)
            return 0

        transient = any(
            re.search(pattern, output, flags=re.IGNORECASE | re.MULTILINE)
            for pattern in TRANSIENT_PATTERNS
        )
        if not transient:
            print(
                "[retry] deterministic build/test failure detected; "
                "stop retrying so the source error can be fixed.",
                file=sys.stderr,
                flush=True,
            )
            return completed.returncode or 1
        if attempt == options.attempts:
            print(
                f"[retry] transient failure persisted after {options.attempts} attempts.",
                file=sys.stderr,
                flush=True,
            )
            return completed.returncode or 1
        wait = options.delay * attempt
        print(
            f"[retry] transient infrastructure failure; retrying in {wait}s.",
            flush=True,
        )
        time.sleep(wait)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
