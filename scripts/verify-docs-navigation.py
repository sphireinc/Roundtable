#!/usr/bin/env python3
"""Verify the configured sidebar labels and targets on every rendered page."""

from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urljoin, urlparse

import yaml


ROOT = Path(__file__).resolve().parent.parent


def rendered_path(source):
    path = Path(source)
    if path.name in ("README.md", "index.md"):
        return (path.parent / "index.html").as_posix()
    return (path.with_suffix("") / "index.html").as_posix()


def configured_entries(items, parents=()):
    entries = []
    for item in items:
        for label, value in item.items():
            ancestry = parents + (label,)
            if isinstance(value, list):
                entries.append((ancestry, None))
                entries.extend(configured_entries(value, ancestry))
            else:
                entries.append((ancestry, rendered_path(value)))
    return entries


class Sidebar(HTMLParser):
    def __init__(self, page):
        super().__init__()
        self.page = page
        self.nav_depth = 0
        self.secondary_depth = 0
        self.capture = None
        self.entries = []
        self.primary_count = 0
        self.items = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        classes = attrs.get("class", "").split()
        if tag == "nav":
            if self.nav_depth:
                self.nav_depth += 1
                if "md-nav--secondary" in classes:
                    self.secondary_depth = self.nav_depth
            elif "md-nav--primary" in classes:
                self.nav_depth = 1
                self.primary_count += 1
        if self.nav_depth and not self.secondary_depth and tag == "li":
            self.items.append({"label": None})
        if (
            self.nav_depth
            and not self.secondary_depth
            and tag in ("a", "label")
            and "md-nav__link" in classes
            and attrs.get("for") != "__toc"
        ):
            self.capture = (tag, attrs.get("href"), [])

    def handle_data(self, data):
        if self.capture:
            self.capture[2].append(data)

    def handle_endtag(self, tag):
        if self.capture and tag == self.capture[0]:
            _, href, fragments = self.capture
            target = None
            if href is not None:
                resolved = urlparse(urljoin("https://docs.invalid/" + self.page, href))
                if resolved.netloc != "docs.invalid" or resolved.fragment:
                    raise ValueError(f"Unexpected sidebar target: {href}")
                target = unquote(resolved.path).lstrip("/")
                if target.endswith("/") or not target:
                    target += "index.html"
            label = " ".join("".join(fragments).split())
            if self.items:
                self.items[-1]["label"] = label
            ancestry = tuple(item["label"] for item in self.items if item["label"])
            self.entries.append((ancestry, target))
            self.capture = None
        if self.nav_depth and not self.secondary_depth and tag == "li":
            if self.items:
                self.items.pop()
        if tag == "nav" and self.nav_depth:
            if self.secondary_depth == self.nav_depth:
                self.secondary_depth = 0
            self.nav_depth -= 1


def main():
    config = yaml.safe_load((ROOT / "mkdocs.yml").read_text())
    if not config.get("use_directory_urls", True):
        raise SystemExit("Navigation verifier requires use_directory_urls: true")
    site = ROOT / config["site_dir"]
    expected = configured_entries(config["nav"])
    for _, target in expected:
        if target and not (site / target).is_file():
            raise SystemExit(f"Configured page has no rendered output: {target}")
    pages = sorted(site.rglob("*.html"))
    if not pages:
        raise SystemExit("No rendered pages found; build the site first")
    failures = []
    for path in pages:
        page = path.relative_to(site).as_posix()
        sidebar = Sidebar(page)
        sidebar.feed(path.read_text())
        if sidebar.primary_count != 1 or sidebar.entries != expected:
            failures.append(page)
            print(f"Sidebar mismatch: {page}")
    if failures:
        raise SystemExit(f"Navigation differs on {len(failures)} pages")
    print(f"Verified {len(expected)} navigation entries on all {len(pages)} HTML pages")


if __name__ == "__main__":
    main()
