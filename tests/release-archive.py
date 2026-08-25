#!/usr/bin/env python3

from __future__ import annotations

import gzip
from pathlib import Path
import os
import struct
import subprocess
import sys
import tarfile
import tempfile


EPOCH = 1_700_000_000


def create_fixture(parent: Path, reverse: bool, host_mtime: int) -> Path:
    root = parent / "userland-1.2.3"
    directories = [root / "bin", root / "cfg"]
    for directory in reversed(directories) if reverse else directories:
        directory.mkdir(parents=True, exist_ok=True)

    files = [
        (root / "README.md", "userland\n", 0o644),
        (root / "bin" / "userland", "#!/bin/sh\nexit 0\n", 0o755),
        (root / "cfg" / "settings", "jobs = 4\n", 0o644),
    ]
    for path, contents, mode in reversed(files) if reverse else files:
        path.write_text(contents)
        path.chmod(mode)

    (root / "current").symlink_to("bin/userland")
    for path in root.rglob("*"):
        if not path.is_symlink():
            os.utime(path, (host_mtime, host_mtime))
    os.utime(root, (host_mtime, host_mtime))
    return root


def main() -> None:
    repository_root = Path(__file__).resolve().parent.parent
    archiver = repository_root / "release" / "create-archive.py"

    with tempfile.TemporaryDirectory(prefix="userland-archive-test.") as temporary:
        work = Path(temporary)
        first_root = create_fixture(work / "first", reverse=False, host_mtime=100)
        second_root = create_fixture(work / "second", reverse=True, host_mtime=2_000_000_000)
        first_archive = work / "first.tar.gz"
        second_archive = work / "second.tar.gz"

        for source, output in ((first_root, first_archive), (second_root, second_archive)):
            subprocess.run(
                [sys.executable, str(archiver), str(source), str(output), str(EPOCH)],
                check=True,
            )

        assert first_archive.read_bytes() == second_archive.read_bytes(), "archives differ"
        assert struct.unpack("<I", first_archive.read_bytes()[4:8])[0] == EPOCH

        with gzip.open(first_archive, "rb") as uncompressed:
            with tarfile.open(fileobj=uncompressed, mode="r:") as archive:
                members = archive.getmembers()

        assert [member.name for member in members] == [
            "userland-1.2.3",
            "userland-1.2.3/bin",
            "userland-1.2.3/cfg",
            "userland-1.2.3/README.md",
            "userland-1.2.3/current",
            "userland-1.2.3/bin/userland",
            "userland-1.2.3/cfg/settings",
        ]
        for member in members:
            assert member.mtime == EPOCH
            assert member.uid == 0
            assert member.gid == 0
            assert member.uname == ""
            assert member.gname == ""

    print("archive reproducibility tests passed")


if __name__ == "__main__":
    main()
