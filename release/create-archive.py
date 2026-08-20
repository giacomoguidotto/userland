#!/usr/bin/env python3
"""Create a gzip-compressed tar archive with normalized metadata."""

from __future__ import annotations

import argparse
import gzip
import os
from pathlib import Path
import tarfile


def normalized(info: tarfile.TarInfo, epoch: int) -> tarfile.TarInfo:
    info.uid = 0
    info.gid = 0
    info.uname = ""
    info.gname = ""
    info.mtime = epoch
    info.pax_headers = {}
    return info


def paths_in_order(root: Path) -> list[Path]:
    paths = [root]
    for directory, directory_names, file_names in os.walk(root):
        directory_names.sort()
        file_names.sort()
        current = Path(directory)
        paths.extend(current / name for name in directory_names)
        paths.extend(current / name for name in file_names)
    return paths


def create_archive(source: Path, output: Path, epoch: int) -> None:
    source = source.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)

    with output.open("wb") as raw_output:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw_output, mtime=epoch) as compressed:
            with tarfile.open(fileobj=compressed, mode="w|", format=tarfile.GNU_FORMAT) as archive:
                for path in paths_in_order(source):
                    archive.add(
                        path,
                        arcname=path.relative_to(source.parent).as_posix(),
                        recursive=False,
                        filter=lambda info: normalized(info, epoch),
                    )


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("epoch", type=int)
    arguments = parser.parse_args()

    if arguments.epoch < 0:
        parser.error("epoch must be non-negative")
    if not arguments.source.is_dir():
        parser.error("source must be a directory")

    create_archive(arguments.source, arguments.output, arguments.epoch)


if __name__ == "__main__":
    main()

