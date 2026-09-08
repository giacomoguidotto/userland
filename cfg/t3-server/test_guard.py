import importlib.machinery
import importlib.util
import pathlib
import tempfile
import unittest

loader = importlib.machinery.SourceFileLoader(
    "guard", str(pathlib.Path(__file__).with_name("t3-singleton-guard"))
)
spec = importlib.util.spec_from_loader(loader.name, loader)
guard = importlib.util.module_from_spec(spec)
loader.exec_module(guard)


class GuardTests(unittest.TestCase):
    def test_both_launch_formats_have_the_same_owner(self):
        home = pathlib.Path("/home/test")
        self.assertEqual(
            guard.server_base(
                [
                    "node",
                    "/home/test/.npm/_npx/abc/node_modules/.bin/t3",
                    "serve",
                    "--port",
                    "3774",
                    "--base-dir",
                    "/home/test/.t3",
                ],
                {},
                home,
            ),
            (home / ".t3").resolve(),
        )
        self.assertEqual(
            guard.server_base(
                [
                    "/usr/bin/node",
                    "/home/test/.t3/runtime/versions/0.0.40/node_modules/t3/dist/bin.mjs",
                    "serve",
                ],
                {"T3CODE_HOME": "/home/test/.t3"},
                home,
            ),
            (home / ".t3").resolve(),
        )

    def test_other_state_and_non_server_commands_are_distinct(self):
        home = pathlib.Path("/home/test")
        self.assertEqual(
            guard.server_base(
                ["node", "/x/node_modules/.bin/t3", "serve", "--base-dir=/tmp/other"],
                {},
                home,
            ),
            pathlib.Path("/tmp/other").resolve(),
        )
        self.assertIsNone(
            guard.server_base(["node", "/x/node_modules/.bin/t3", "connect"], {}, home)
        )
        self.assertIsNone(guard.server_base(["node", "/x/bin.mjs", "serve"], {}, home))

    def test_managed_service_wins_and_old_listener_is_reported(self):
        keeper, duplicates = guard.choose_managed(
            [{"pid": 10, "managed": False}, {"pid": 20, "managed": True}]
        )
        self.assertEqual(keeper["pid"], 20)
        self.assertEqual(duplicates, [10])

    def test_missing_or_ambiguous_managed_worker_does_not_choose_a_victim(self):
        for servers in [
            [],
            [{"pid": 10, "managed": False}],
            [{"pid": 10, "managed": True}, {"pid": 20, "managed": True}],
        ]:
            with self.assertRaises(RuntimeError):
                guard.choose_managed(servers)

    def test_unchanged_routing_is_not_rewritten(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "backend-port"
            guard.atomic_text(path, "37437\n")
            before = path.stat().st_mtime_ns
            guard.atomic_text(path, "37437\n")
            self.assertEqual(before, path.stat().st_mtime_ns)
            guard.atomic_text(path, "40000\n")
            self.assertEqual(path.read_text(), "40000\n")


if __name__ == "__main__":
    unittest.main()
