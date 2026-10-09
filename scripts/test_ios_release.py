import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('ios_release', Path(__file__).with_name('ios-release.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseWorkflowTests(unittest.TestCase):
    def setUp(self):
        self.env = patch.dict(os.environ, {}, clear=True)
        self.env.start()
        self.addCleanup(self.env.stop)
        self.fixture = {'ipaSha256': 'a' * 64, 'version': '0.1.0', 'buildNumber': '3'}

    def test_rejects_replaced_artifact_before_upload_or_resume(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = Path(directory) / 'Release.json'
            manifest.write_text(json.dumps(self.fixture))
            with patch.object(release, 'MANIFEST', manifest), patch.object(release, 'verify', return_value=self.fixture):
                os.environ['RETTY_EXPECTED_SHA256'] = 'b' * 64
                with self.assertRaisesRegex(ValueError, 'changed since this workflow checkpoint'):
                    release.check()
                os.environ.pop('RETTY_EXPECTED_SHA256')
                manifest.write_text(json.dumps({**self.fixture, 'buildNumber': '2'}))
                with self.assertRaisesRegex(ValueError, 'manifest mismatch'):
                    release.check()

    def test_refuses_app_id_for_another_bundle(self):
        os.environ['ASC_APP_ID'] = '123'
        with patch.object(release, 'config', return_value={'identifier': 'com.daodao.retty'}), \
             patch.object(release, 'asc', return_value={'data': {'id': '123', 'attributes': {'bundleId': 'other.app'}}}):
            with self.assertRaisesRegex(ValueError, 'does not match'):
                release.resolve_app()

    def test_successful_upload_receipt_is_reused(self):
        with tempfile.TemporaryDirectory() as directory:
            receipt = Path(directory) / 'Upload.json'
            previous = {**self.fixture, 'appId': '123', 'receipt': {'accepted': True}}
            receipt.write_text(json.dumps(previous))
            with patch.object(release, 'RECEIPT', receipt), \
                 patch.object(release, 'check', return_value=self.fixture), \
                 patch.object(release, 'resolve_app', return_value='123'), \
                 patch.object(release, 'asc') as client:
                self.assertEqual(release.upload(), previous)
                client.assert_not_called()

    def test_receipt_for_a_different_ipa_does_not_skip_upload(self):
        with tempfile.TemporaryDirectory() as directory:
            receipt = Path(directory) / 'Upload.json'
            receipt.write_text(json.dumps({'ipaSha256': 'b' * 64, 'appId': '123'}))
            with patch.object(release, 'RECEIPT', receipt), \
                 patch.object(release, 'check', return_value=self.fixture), \
                 patch.object(release, 'resolve_app', return_value='123'), \
                 patch.object(release, 'asc', return_value={'accepted': True}) as client:
                result = release.upload()
                self.assertEqual(result['ipaSha256'], self.fixture['ipaSha256'])
                self.assertEqual(client.call_args.args[:2], ('builds', 'upload'))
                self.assertEqual(json.loads(receipt.read_text()), result)

    def test_wait_selects_exact_version_and_build(self):
        os.environ['RETTY_APP_ID'] = '123'
        with patch.object(release, 'check', return_value=self.fixture), \
             patch.object(release, 'asc', return_value={'buildId': 'build-3'}) as client:
            self.assertEqual(release.wait(), {'buildId': 'build-3'})
            args = client.call_args.args
            self.assertNotIn('--latest', args)
            self.assertIn('--fail-on-invalid', args)
            self.assertEqual(args[args.index('--build-number') + 1], '3')
            self.assertEqual(args[args.index('--version') + 1], '0.1.0')

    def test_offline_build_number_does_not_contact_apple(self):
        os.environ['BUILD_NUMBER'] = '3'
        with patch.object(release, 'config', return_value={'version': '0.1.0'}), \
             patch.object(release, 'asc') as client:
            self.assertEqual(release.context(), {'version': '0.1.0', 'buildNumber': '3'})
            client.assert_not_called()
            os.environ['BUILD_NUMBER'] = '3; unexpected-command'
            with self.assertRaises(ValueError):
                release.context()

    def test_no_implicit_testflight_group(self):
        with self.assertRaisesRegex(ValueError, 'TESTFLIGHT_GROUP is required'):
            release.require_group()


if __name__ == '__main__':
    unittest.main()
