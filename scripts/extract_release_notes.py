#!/usr/bin/env python3
"""Extract one version's update section from CHANGELOG.md for a GitHub Release."""

import os
import re
from pathlib import Path

version = os.environ.get("DTV_RELEASE_VERSION", "").strip()
if not re.fullmatch(r"\d+\.\d+\.\d+", version):
    raise SystemExit("DTV_RELEASE_VERSION must be a semantic version such as 1.0.7")

lines = Path("CHANGELOG.md").read_text(encoding="utf-8").splitlines()
start = None
for index, line in enumerate(lines):
    if re.match(r"^##\s+" + re.escape(version) + r"(?:（|\s|\()", line):
        start = index
        break
if start is None:
    raise SystemExit("CHANGELOG.md has no section for " + version)

end = start + 1
while end < len(lines) and not lines[end].startswith("## "):
    end += 1
section = "\n".join(lines[start + 1:end]).strip()
if not section:
    raise SystemExit("CHANGELOG section is empty for " + version)

# Keep the source section as authored, including headings, caveats and validation notes.
delimiter = "DTV_CHANGELOG_" + version.replace(".", "_")
if delimiter in section:
    raise SystemExit("Output delimiter unexpectedly appears in changelog section")
with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
    output.write("notes<<{}\n{}\n{}\n".format(delimiter, section, delimiter))
print("Extracted CHANGELOG section for D TV " + version)
