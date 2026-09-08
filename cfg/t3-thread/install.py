#!/usr/bin/env python3
"""Install only this controller and its skill; no global convergence or T3 restart."""

import argparse, fcntl, hashlib, json, os, pathlib, re, shutil, subprocess, tempfile

parser = argparse.ArgumentParser()
parser.add_argument("--profile", choices=["mac", "vm"], required=True)
args = parser.parse_args()
source = pathlib.Path(__file__).resolve().parent
skill = source.parent / "agents/skills/t3-code-threads"
home = pathlib.Path.home()
data = (
    pathlib.Path(os.environ.get("XDG_DATA_HOME", home / ".local/share")) / "t3-thread"
)
data.mkdir(parents=True, exist_ok=True, mode=0o700)
files = [
    p
    for p in source.rglob("*")
    if p.is_file()
    and "node_modules" not in p.parts
    and "__pycache__" not in p.parts
    and not p.name.startswith("._")
    and p.name != ".DS_Store"
]
digest = hashlib.sha256()
for p in sorted(files):
    digest.update(str(p.relative_to(source)).encode() + p.read_bytes())
for p in sorted(skill.rglob("*")):
    if p.is_file() and not p.name.startswith("._") and p.name != ".DS_Store":
        digest.update(str(p.relative_to(skill)).encode() + p.read_bytes())
release = data / "releases" / digest.hexdigest()[:20]
node = shutil.which("node")
if not node:
    candidates = [
        p
        for p in (home / ".local/share/mise/installs/node").glob("*/bin/node")
        if re.fullmatch(r"\d+\.\d+\.\d+", p.parent.parent.name)
    ]
    candidates.sort(
        key=lambda p: tuple(int(x) for x in p.parent.parent.name.split(".")),
        reverse=True,
    )
    node = str(candidates[0]) if candidates else None
if not node:
    raise SystemExit("Install Node 24 or newer first.")
major = int(
    subprocess.check_output(
        [node, "-p", 'process.versions.node.split(".")[0]'], text=True
    ).strip()
)
if major < 24:
    raise SystemExit("Node 24 or newer is required.")
npm = pathlib.Path(node).parent / "npm"
if not npm.exists():
    raise SystemExit("npm must be installed beside Node.")
env = dict(
    os.environ,
    PATH=str(pathlib.Path(node).parent) + os.pathsep + os.environ.get("PATH", ""),
)


def link(target, path):
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink() and path.resolve() == target.resolve():
        return
    if path.exists() or path.is_symlink():
        allowed = path.is_symlink() and (
            path == data / "node"
            or path.resolve().is_relative_to(data)
            or path.resolve().is_relative_to(source.parent)
        )
        if not allowed:
            raise SystemExit(f"Refusing to replace an unmanaged file: {path}")
    tmp = path.with_name(path.name + ".t3-thread-new")
    if tmp.is_symlink():
        tmp.unlink()
    tmp.symlink_to(target)
    os.replace(tmp, path)


with (data / "install.lock").open("a") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    if not release.exists():
        release.parent.mkdir(parents=True, exist_ok=True)
        stage = pathlib.Path(tempfile.mkdtemp(prefix=".install-", dir=release.parent))
        try:
            for p in files:
                dest = stage / p.relative_to(source)
                dest.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(p, dest)
            shutil.copytree(
                skill,
                stage / "skill",
                ignore=shutil.ignore_patterns("._*", ".DS_Store"),
            )
            subprocess.run(
                [
                    str(npm),
                    "ci",
                    "--ignore-scripts",
                    "--omit=dev",
                    "--no-audit",
                    "--no-fund",
                ],
                cwd=stage,
                env=env,
                check=True,
            )
            subprocess.run(
                [node, "--test", "test/journal.test.mjs", "test/commands.test.mjs"],
                cwd=stage,
                env=env,
                check=True,
            )
            os.replace(stage, release)
        finally:
            if stage.exists():
                shutil.rmtree(stage)
    link(pathlib.Path(node), data / "node")
    # Verify all install destinations before switching the current release.
    destinations = [
        home / ".local/bin/t3-thread",
        home / ".config/t3-thread/config.json",
    ]
    skill_dirs = [
        home / ".agents/skills",
        home / ".codex/skills",
        home / ".claude/skills",
    ]
    for pattern in [".codex-t3/*/skills", ".claude-t3/*/skills"]:
        skill_dirs.extend(p for p in home.glob(pattern) if p.is_dir())
    for path in destinations + [p / "t3-code-threads" for p in skill_dirs]:
        if path.exists() or path.is_symlink():
            if not (
                path.is_symlink()
                and (
                    path.resolve().is_relative_to(data)
                    or path.resolve().is_relative_to(source.parent)
                )
            ):
                raise SystemExit(f"Refusing to replace an unmanaged file: {path}")
    link(release, data / "current")
    link(data / "current/bin/t3-thread", home / ".local/bin/t3-thread")
    config = (
        source / "profiles" / f"{args.profile}.json"
        if args.profile == "mac"
        else data / "current/profiles/vm.json"
    )
    link(config, home / ".config/t3-thread/config.json")
    for directory in skill_dirs:
        link(data / "current/skill", directory / "t3-code-threads")
print(
    json.dumps(
        {
            "release": str(release),
            "profile": args.profile,
            "node": node,
            "skills_installed": len(skill_dirs),
        }
    )
)
