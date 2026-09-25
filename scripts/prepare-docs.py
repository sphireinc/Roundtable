#!/usr/bin/env python3
"""Assemble the curated documentation sources without copying project code."""

from pathlib import Path
import shutil


ROOT = Path(__file__).resolve().parent.parent
DESTINATION = ROOT / ".docs-build"


def copy_markdown_tree(source: Path, destination: Path) -> None:
    for path in sorted(source.rglob("*.md")):
        relative = path.relative_to(ROOT)
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)


def main() -> None:
    if DESTINATION.exists():
        shutil.rmtree(DESTINATION)
    DESTINATION.mkdir()

    for path in (
        ROOT / "README.md",
        ROOT / "AGENTS.ROUNDTABLE.md",
        ROOT / "PROJECT.ROUNDTABLE.md",
        ROOT / "POLICIES.ROUNDTABLE.md",
    ):
        shutil.copy2(path, DESTINATION / path.name)

    for directory in (ROOT / "docs", ROOT / "api" / "docs"):
        copy_markdown_tree(directory, DESTINATION)

    for path in (
        ROOT / "api" / "README.md",
        ROOT / "api" / "openapi.yaml",
        ROOT / "api" / "examples" / "dashboard-fixtures.json",
        ROOT / "examples" / "resume-briefing.md",
    ):
        target = DESTINATION / path.relative_to(ROOT)
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(path, target)


if __name__ == "__main__":
    main()
