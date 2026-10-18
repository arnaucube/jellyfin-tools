"""Exercise helper startup and export failures without production services."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class GenerateHelperTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="reel-helper-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "scripts").mkdir()
        (self.root / "bin").mkdir()
        for name in ("generate-review.sh", "export-jellystat.sh", "jellystat-export.sql"):
            shutil.copy2(Path(__file__).parent / name, self.root / "scripts" / name)
        for name in ("dirname", "mkdir", "date", "mktemp", "mv", "rm"):
            (self.root / "bin" / name).symlink_to(shutil.which(name))
        (self.root / ".env").write_text(
            "JELLYFIN_URL=http://fixture.invalid\n"
            "JELLYFIN_API_KEY=synthetic-key-do-not-log\n"
            "PGPASSWORD='synthetic password $dollar #hash !'\n"
        )
        self.args = self.root / "args"
        self.env = {**os.environ, "PATH": str(self.root / "bin"), "REEL_CHECK_ARGS": str(self.args)}
        self.command("go", 'printf "%s\\n" "$@" > "$REEL_CHECK_ARGS"\n')
        self.command("psql", 'printf "%s\\n" "{}"\n')

    def command(self, name, script):
        path = self.root / "bin" / name
        path.write_text("#!/bin/sh\n" + script)
        path.chmod(0o755)

    def run_helper(self, year="2026"):
        result = subprocess.run(
            ["/bin/sh", str(self.root / "scripts/generate-review.sh"), year],
            env=self.env, capture_output=True, text=True, timeout=5,
        )
        self.assertNotIn("synthetic-key-do-not-log", result.stdout + result.stderr)
        self.assertNotIn("synthetic password", result.stdout + result.stderr)
        return result

    def test_missing_dependencies_report_error_before_creating_data(self):
        for name in ("go", "psql"):
            with self.subTest(command=name):
                path = self.root / "bin" / name
                original = path.read_text()
                path.unlink()
                result = self.run_helper()
                self.assertEqual(result.returncode, 127)
                self.assertIn(f"Missing required command: {name}", result.stderr)
                self.assertFalse((self.root / "data").exists())
                self.assertFalse(self.args.exists())
                path.write_text(original)
                path.chmod(0o755)

    def test_success_reports_stages_and_forwards_year(self):
        result = self.run_helper()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Preparing recap generation for 2026", result.stderr)
        self.assertIn("Exporting Jellystat history", result.stderr)
        self.assertIn("downloading Jellyfin posters", result.stderr)
        self.assertEqual(self.args.read_text().splitlines()[:5], ["run", "./cmd/review", "generate", "--year", "2026"])
        self.assertEqual((self.root / "input/jellystat.jsonl").read_text(), "{}\n")
        self.assertTrue((self.root / "data").is_dir())
        self.assertEqual((self.root / "data").stat().st_mode & 0o777, 0o700)

    def test_export_failure_preserves_history_and_stops_generation(self):
        (self.root / "input").mkdir()
        history = self.root / "input/jellystat.jsonl"
        history.write_text("previous successful export\n")
        self.command("psql", 'echo "synthetic database error" >&2\nexit 7\n')
        result = self.run_helper()
        self.assertEqual(result.returncode, 7)
        self.assertIn("History export failed", result.stderr)
        self.assertFalse(self.args.exists())
        self.assertEqual(history.read_text(), "previous successful export\n")
        self.assertFalse(list((self.root / "input").glob("*.tmp.*")))

    def test_postgres_never_waits_for_a_password_prompt(self):
        self.command("psql", 'if [ "$1" != "--no-password" ]; then exit 9; fi\nprintf "%s\\n" "{}"\n')
        result = self.run_helper()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_password_reaches_postgres_through_environment_only(self):
        self.command("psql", '''
if [ "$PGPASSWORD" != 'synthetic password $dollar #hash !' ]; then exit 8; fi
for argument do
  case "$argument" in *"$PGPASSWORD"*) exit 9 ;; esac
done
printf '%s\\n' '{}'
''')
        result = self.run_helper()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_empty_password_stops_export_without_replacing_history(self):
        with (self.root / ".env").open("a") as config:
            config.write("PGPASSWORD=''\n")
        (self.root / "input").mkdir()
        history = self.root / "input/jellystat.jsonl"
        history.write_text("previous successful export\n")
        self.command("psql", 'echo "psql should not run" >&2\nexit 9\n')
        result = self.run_helper()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Set PGPASSWORD", result.stderr)
        self.assertNotIn("psql should not run", result.stderr)
        self.assertFalse(self.args.exists())
        self.assertEqual(history.read_text(), "previous successful export\n")
        self.assertFalse(list((self.root / "input").glob("*.tmp.*")))

    def test_invalid_year_does_not_start_work(self):
        result = self.run_helper("bad-year")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Year must be", result.stderr)
        self.assertFalse(self.args.exists())
        self.assertFalse((self.root / "data").exists())


if __name__ == "__main__":
    unittest.main()
