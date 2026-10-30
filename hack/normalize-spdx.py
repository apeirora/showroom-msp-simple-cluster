#!/usr/bin/env python3
"""Compare complete SPDX documents, ignoring only known volatile metadata."""

import json
import sys
from pathlib import Path


def normalize(value):
    """Return JSON data with object keys and unordered SPDX arrays sorted.

    The input is not modified. JSON serialization errors propagate; there are
    no filesystem or other side effects.
    """
    if isinstance(value, dict):
        return {key: normalize(item) for key, item in sorted(value.items())}
    if isinstance(value, list):
        return sorted((normalize(item) for item in value),
                      key=lambda item: json.dumps(item, sort_keys=True))
    return value


def load_documents(directory):
    """Return normalized SPDX documents keyed by filename from directory.

    Require one SPDX document for each CycloneDX document. Missing, extra,
    invalid, or unreadable files raise an error. Reads files without modifying
    them and removes only the document namespace and creation timestamp.
    """
    directory = Path(directory)
    spdx = {p.name.removesuffix(".spdx.json"): p for p in directory.glob("*.spdx.json")}
    cdx = {p.name.removesuffix(".cdx.json") for p in directory.glob("*.cdx.json")}
    if not cdx or set(spdx) != cdx:
        raise ValueError(f"SPDX/CycloneDX document mismatch in {directory}: "
                         f"missing SPDX {sorted(cdx - set(spdx))}, "
                         f"extra SPDX {sorted(set(spdx) - cdx)}")
    documents = {}
    for name, path in sorted(spdx.items()):
        with path.open(encoding="utf-8") as source:
            document = json.load(source)
        document.pop("documentNamespace", None)
        document.get("creationInfo", {}).pop("created", None)
        documents[name] = normalize(document)
    return documents


def main():
    """Print normalized documents for the directory argument; errors propagate."""
    if len(sys.argv) != 2:
        raise ValueError("expected one SBOM directory")
    print(json.dumps(load_documents(sys.argv[1]), indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
