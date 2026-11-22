#!/usr/bin/env python3

import argparse
import hashlib
import json
import re
import subprocess
from collections import defaultdict
from pathlib import Path
from urllib.request import urlopen


ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "THIRD_PARTY_LICENSES.txt"
NOTICE_FILE = re.compile(
    r"^(licen[cs]e|copying|notice|copyright|unlicense|patents)([._-].*)?$", re.I
)
LICENSE_FILE = re.compile(r"^(licen[cs]e|copying|unlicense)([._-].*)?$", re.I)
PROMETHEUS_GENERATED = (
    "buf.build/gen/go/prometheus/prometheus/protocolbuffers/go"
    "@v1.36.12-20261001224011-d7f6cfecf2b8.2"
)
PROMETHEUS_SOURCE = (
    "https://raw.githubusercontent.com/prometheus/prometheus/"
    "7c741e2879db6b58b8d152f566a8112f0d642bb1/"
)
PROMETHEUS_FILES = {
    "LICENSE": "c71d239df91726fc519c6eb72d318ec65820627232b2f796219e87dcf35d0ab4",
    "NOTICE": "ac9e304462a58a4d71b6488423d4447e614c8b31d517fe24345016522c102a78",
}


def go(*args):
    return subprocess.check_output(["go", *args], cwd=ROOT, text=True)


def modules():
    module_format = "{{if not .Main}}{{.Path}} {{.Version}}{{end}}"
    deps = [
        line.replace(" ", "@")
        for line in go("list", "-m", "-f", module_format, "all").splitlines()
        if line
    ]
    build_script = (ROOT / "build.sh").read_text()
    builders = set(re.findall(r"go\.k6\.io/xk6/cmd/xk6@(v[\w.-]+)", build_script))
    if len(builders) != 1:
        raise ValueError("Could not identify the pinned xk6 version in build.sh")
    deps.append(f"go.k6.io/xk6@{builders.pop()}")
    decoder = json.JSONDecoder()
    response = go("mod", "download", "-json", *sorted(set(deps))).lstrip()
    while response:
        module, end = decoder.raw_decode(response)
        yield f"{module['Path']}@{module['Version']}", Path(module["Dir"])
        response = response[end:].lstrip()


def notices(module, directory):
    found = False
    for path in sorted(directory.rglob("*")):
        if not path.is_file() or not NOTICE_FILE.fullmatch(path.name):
            continue
        if path.suffix in {".go", ".prototext"}:
            continue
        found |= bool(LICENSE_FILE.fullmatch(path.name))
        yield str(path.relative_to(directory)), path.read_bytes()
    if found:
        return

    # This generated module carries Apache-2.0 headers but no standalone license.
    if module != PROMETHEUS_GENERATED:
        raise ValueError(
            f"No license text found for {module}; review it before updating this inventory"
        )
    for name, digest in PROMETHEUS_FILES.items():
        url = PROMETHEUS_SOURCE + name
        with urlopen(url, timeout=30) as response:
            text = response.read()
        if hashlib.sha256(text).hexdigest() != digest:
            raise ValueError(f"Checksum mismatch for {url}")
        yield f"{name} (upstream Prometheus: {url})", text


def inventory():
    texts = defaultdict(list)
    for module, directory in sorted(modules()):
        for path, text in notices(module, directory):
            texts[text].append(f"{module}/{path}")
    sections = [
        b"Third-party license texts and notices\n\n"
        b"This inventory covers the pinned extension Go module graph (including test\n"
        b"dependencies) and the xk6 build tool's license. These modules are downloaded\n"
        b"at build time and are not vendored here. Identical texts are listed once.\n"
        b"The Go toolchain, native IBM MQ client, and base and test container images\n"
        b"are obtained separately and retain their own terms.\n"
    ]
    for text, paths in sorted(texts.items(), key=lambda item: item[1]):
        sections.append(b"\n" + b"=" * 72 + b"\n" + "\n".join(paths).encode() + b"\n\n" + text)
        if not text.endswith(b"\n"):
            sections.append(b"\n")
    return b"".join(sections)


def main():
    parser = argparse.ArgumentParser(description="Collect upstream Go license texts and notices.")
    parser.add_argument(
        "--check", action="store_true", help="Check the committed inventory without changing it"
    )
    args = parser.parse_args()
    content = inventory()
    if args.check:
        if not OUTPUT.is_file() or OUTPUT.read_bytes() != content:
            parser.exit(
                1, "Run python3 scripts/update-third-party-licenses.py to refresh the inventory.\n"
            )
    else:
        OUTPUT.write_bytes(content)


if __name__ == "__main__":
    main()
