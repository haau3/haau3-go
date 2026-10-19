"""scripts/check_public_text.py finds each kind of text this repository never publishes, and allows the
shapes its own text uses. The samples are made up.

    python3 -I scripts/test_check_public_text.py
"""

from __future__ import annotations

import importlib.util
import os
import subprocess
import sys
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
_spec = importlib.util.spec_from_file_location("check_public_text", os.path.join(ROOT, "scripts", "check_public_text.py"))
assert _spec and _spec.loader
check_public_text = importlib.util.module_from_spec(_spec)
sys.modules["check_public_text"] = check_public_text
_spec.loader.exec_module(check_public_text)


def found(text: str) -> list[str]:
    return [f.match for f in check_public_text.check("sample", text)]


class CheckPublicTextTest(unittest.TestCase):
    def test_it_finds_each_kind(self) -> None:
        for text, match in [
            ("see haau3/private-thing for why", "haau3/private-thing"),
            ("at https://github.com/haau3/private-thing/issues", "github.com/haau3/private-thing"),
            ("git@github.com:haau3/private-thing.git", "github.com:haau3/private-thing.git"),
            ("it runs in haau3-example-project", "haau3-example-project"),
            ("decided in issue #123", "#123"),
            ("see issue 123", "issue 123"),
            ("this is ABC-4", "ABC-4"),
            ("part of the milestone", "milestone"),
            ("the owner approves it", "the owner"),
            ("after each deploy", "deploy"),
            ("the key is an org secret", "org secret"),
            ("Iv23liEXAMPLEexample00", "Iv23liEXAMPLEexample00"),
            ("write to someone@personal.example-mail.net", "someone@personal.example-mail.net"),
        ]:
            with self.subTest(text=text):
                self.assertIn(match, found(text))

    def test_it_allows_what_this_repositorys_own_text_uses(self) -> None:
        text = "\n".join(
            [
                'go get github.com/haau3/haau3-go, imported as "github.com/haau3/haau3-go/fhir".',
                "From haau3/haau3-go#3, opened by haau3-bot[bot], sent as haau3-go/0.1.0 (go 1.26).",
                "Also for TypeScript: haau3-node at https://github.com/haau3/haau3-node, and haau3-python.",
                "PHQ-9 and UTF-8, the plan H2406-013, golang/go#61405 and Go 1.26.9.",
                "* the API's spec at 1.0.0-preview.4 ([#5](https://github.com/haau3/haau3-go/issues/5))",
                "Write to security@haau3.com. Co-Authored-By: Claude <noreply@anthropic.com>",
                "A base URL such as https://proxy.example.test/haau3/v1 works.",
            ]
        )
        self.assertEqual(found(text), [])

    def test_a_changelog_link_only_to_this_repository(self) -> None:
        self.assertEqual(found("([#5](https://github.com/haau3/haau3-go/issues/5))"), [])
        self.assertEqual(found("([#5](https://github.com/haau3/haau3-python/issues/5))"), ["#5"])

    def test_extra_terms_from_a_file_of_patterns_with_comments(self) -> None:
        rules = check_public_text.term_rules("# a comment\nsecret-codename\n\n  other\\d+  # trailing comment\n")
        self.assertEqual([r.pattern.pattern for r in rules], ["secret-codename", "other\\d+"])
        matches = [f.match for f in check_public_text.check("sample", "the Secret-Codename and other42", rules)]
        self.assertEqual(matches, ["Secret-Codename", "other42"])

    def test_generated_files_and_test_data_are_not_ours(self) -> None:
        def not_ours(path: str) -> bool:
            return any(p.search(path) for p in check_public_text.NOT_OURS)

        for path in ["operations_gen.go", "operations_gen_test.go", "fhir/resources_gen.go", "openapi/openapi.json", "hl7/fhir.schema.json.zip",
                     "api.md", "examples/go.sum", "testdata/answer.json"]:
            with self.subTest(path=path):
                self.assertTrue(not_ours(path))
        for path in ["client.go", "fhir/resource.go", "README.md", "examples/gin/main.go", "go.mod"]:
            with self.subTest(path=path):
                self.assertFalse(not_ours(path))

    def test_a_repositorys_first_push_checks_every_commit(self) -> None:
        self.assertEqual(check_public_text.commit_range("no-such-ref-anywhere..HEAD"), "HEAD")
        self.assertEqual(check_public_text.commit_range("HEAD..HEAD"), "HEAD..HEAD")

    def test_this_repositorys_own_files_pass(self) -> None:
        # The real run, as CI and a pull request use it: it fails on any finding.
        result = subprocess.run(
            [sys.executable, "-I", "scripts/check_public_text.py"], cwd=ROOT, capture_output=True, text=True
        )
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
