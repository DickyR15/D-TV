#!/usr/bin/env python3
"""Sign the public D-TV provider compatibility manifest with an Actions secret."""

import base64
import os
from pathlib import Path

from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = ROOT / "config" / "provider-compatibility.json"
SIGNATURE = ROOT / "config" / "provider-compatibility.json.sig"
SECRET_NAME = "DTV_PROVIDER_COMPAT_SIGNING_PRIVATE_KEY"


def main() -> None:
    encoded = os.environ.get(SECRET_NAME, "").strip()
    if not encoded:
        raise SystemExit(f"Missing GitHub Actions secret: {SECRET_NAME}")
    try:
        seed = base64.b64decode(encoded, validate=True)
    except ValueError:
        raise SystemExit(f"{SECRET_NAME} must be Base64-encoded Ed25519 seed bytes") from None
    if len(seed) != 32:
        raise SystemExit(f"{SECRET_NAME} must decode to exactly 32 bytes")

    if not MANIFEST.is_file():
        raise SystemExit("Provider compatibility manifest is missing")
    payload = MANIFEST.read_bytes()
    if not payload or len(payload) > 256 * 1024:
        raise SystemExit("Provider compatibility manifest size is invalid")

    private_key = Ed25519PrivateKey.from_private_bytes(seed)
    signature = base64.b64encode(private_key.sign(payload)).decode("ascii") + "\n"
    SIGNATURE.write_text(signature, encoding="ascii", newline="\n")
    print("Provider compatibility manifest signed (private key not logged).")


if __name__ == "__main__":
    main()
