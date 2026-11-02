#!/usr/bin/env python3

import argparse
import subprocess
from pathlib import Path
from typing import List


def run_git_log(revision_range: str) -> List[str]:
    result = subprocess.run(
        ["git", "log", "--no-merges", "--pretty=format:%s", revision_range],
        check=True,
        capture_output=True,
        text=True,
    )
    return [line.strip() for line in result.stdout.splitlines() if line.strip()]


def build_entry(version: str, release_date: str, commits: List[str]) -> str:
    base_version = version.lstrip("v")
    if not commits:
        commits = ["Automated release"]

    bullets = "".join(f"        * {line}\n" for line in commits)
    return f"{base_version}   {release_date}\n        [Patch]\n{bullets}\n"


def update_changelog(entry: str, changelog_path: Path) -> None:
    contents = changelog_path.read_text(encoding="utf-8").splitlines(keepends=True)

    try:
        insert_index = next(
            idx for idx, line in enumerate(contents) if line.strip() == "```"
        ) + 1
    except StopIteration:
        raise SystemExit("Could not find start of changelog code fence in CHANGELOG.md")

    contents.insert(insert_index, entry)
    changelog_path.write_text("".join(contents), encoding="utf-8")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Update CHANGELOG.md for a release")
    parser.add_argument("--version", required=True, help="Version tag (e.g., v1.2.3)")
    parser.add_argument("--release-date", required=True, help="Release date YYYY-MM-DD")
    parser.add_argument(
        "--previous-tag", default="", help="Previous tag for git log range"
    )
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    changelog_path = Path("CHANGELOG.md")

    revision_range = "HEAD"
    if args.previous_tag:
        revision_range = f"{args.previous_tag}..HEAD"

    commits = run_git_log(revision_range)
    entry = build_entry(args.version, args.release_date, commits)
    update_changelog(entry, changelog_path)


if __name__ == "__main__":
    main()
