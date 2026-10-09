#!/usr/bin/env python3
"""Retty release policy; MyGo builds locally and asc owns remote operations."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import subprocess
import sys
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'build/app-store'
TARGET = OUT / 'ios-arm64'
IPA = TARGET / 'Export/Retty.ipa'
ARCHIVE = TARGET / 'Retty.xcarchive'
SYMBOLS = ARCHIVE / 'dSYMs/MyGoApp.app.dSYM'
MANIFEST = OUT / 'Release.json'
RECEIPT = OUT / 'Upload.json'


def config():
    return json.loads((ROOT / 'mygo.json').read_text())


def invoke(args, **kwargs):
    return subprocess.run(args, cwd=ROOT, check=True, **kwargs)


def asc(*args):
    result = invoke([str(ROOT / 'scripts/asc.sh'), *args, '--output', 'json'],
                    stdout=subprocess.PIPE, text=True)
    return json.loads(result.stdout)


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + '.next')
    temporary.write_text(json.dumps(value, indent=2, ensure_ascii=False) + '\n')
    temporary.replace(path)


def number(value):
    value = str(value)
    if not re.fullmatch(r'[1-9][0-9]*', value):
        raise ValueError('BUILD_NUMBER must be a positive integer.')
    return value


def resolve_app():
    expected = config()['identifier']
    app_id = os.environ.get('ASC_APP_ID')
    if app_id:
        app = asc('apps', 'view', '--id', app_id)['data']
    else:
        apps = asc('apps', 'list', '--bundle-id', expected)['data']
        if len(apps) != 1:
            raise ValueError(f'Expected exactly one App Store Connect app for {expected}.')
        app = apps[0]
    if app['attributes']['bundleId'] != expected:
        raise ValueError('ASC_APP_ID does not match Retty\'s bundle ID.')
    app_id = str(app['id'])
    if not re.fullmatch(r'[0-9]+', app_id):
        raise ValueError('Unexpected App Store Connect app ID format.')
    return app_id


def context():
    c = config()
    value = os.environ.get('BUILD_NUMBER')
    if not value:
        value = asc('builds', 'next-build-number', '--app', resolve_app(),
                    '--version', c['version'], '--platform', 'IOS')['nextBuildNumber']
    return {'version': c['version'], 'buildNumber': number(value)}


def verify():
    if not IPA.is_file() or not SYMBOLS.is_dir():
        raise ValueError('No complete release package. Run scripts/release-ios.sh package first.')
    result = invoke(['go', 'tool', 'mygo', 'ios', 'check', '-ipa', str(IPA),
                     '-symbols', str(SYMBOLS), '-distribution', '-json'],
                    stdout=subprocess.PIPE, text=True, env={**os.environ, 'GOWORK': 'off'})
    report = json.loads(result.stdout)
    if not report.get('distribution') or not all(c['passed'] for c in report['checks']):
        raise ValueError('MyGo distribution preflight failed.')
    c = config()
    with tempfile.TemporaryDirectory(prefix='retty-release-check-') as stage:
        stage = Path(stage)
        # MyGo has already checked ZIP paths, size limits and CRCs above.
        with zipfile.ZipFile(IPA) as package:
            package.extractall(stage)
        app, = (stage / 'Payload').glob('*.app')
        info = plistlib.loads((app / 'Info.plist').read_bytes())
        if info['CFBundleIdentifier'] != c['identifier'] or info['CFBundleShortVersionString'] != c['version']:
            raise ValueError('IPA identity/version differs from mygo.json; repackage before uploading.')
        profile = plistlib.loads(invoke(['security', 'cms', '-D', '-i', str(app / 'embedded.mobileprovision')],
                                       stdout=subprocess.PIPE, stderr=subprocess.DEVNULL).stdout)
        if profile['TeamIdentifier'] != [c['ios']['developmentTeam']]:
            raise ValueError('IPA signing team differs from mygo.json.')
        invoke(['codesign', '-d', '--extract-certificates=' + str(stage / 'cert-'), str(app)],
               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if (stage / 'cert-0').read_bytes() not in profile['DeveloperCertificates']:
            raise ValueError('The embedded profile does not contain the actual signing certificate.')
        imports = invoke(['xcrun', 'nm', '-u', str(app / info['CFBundleExecutable'])],
                         stdout=subprocess.PIPE, text=True).stdout
        for capability, symbol in {
            'camera': '_AVMediaTypeVideo', 'microphone': '_AVMediaTypeAudio',
            'geolocation': '_OBJC_CLASS_$_CLLocationManager',
            'photos': '_OBJC_CLASS_$_PHPhotoLibrary', 'biometrics': '_OBJC_CLASS_$_LAContext',
        }.items():
            if capability not in c['ios'].get('capabilities', []) and symbol in imports:
                raise ValueError(f'Disabled protected API remains in IPA: {capability}.')
        encryption = ('questionnaire' if 'ITSAppUsesNonExemptEncryption' not in info
                      else 'non-exempt' if info['ITSAppUsesNonExemptEncryption'] else 'exempt')
    return {
        'bundleId': report['bundleID'], 'version': report['version'],
        'buildNumber': number(report['buildNumber']), 'teamId': profile['TeamIdentifier'][0],
        'ipa': str(IPA.relative_to(ROOT)), 'archive': str(ARCHIVE.relative_to(ROOT)),
        'symbols': str(SYMBOLS.relative_to(ROOT)),
        'ipaSha256': hashlib.sha256(IPA.read_bytes()).hexdigest(),
        'encryption': encryption, 'distributionChecks': len(report['checks']),
    }


def check():
    actual = verify()
    if not MANIFEST.is_file():
        raise ValueError('Release.json is missing; run the canonical package workflow.')
    recorded = json.loads(MANIFEST.read_text())
    for key, value in actual.items():
        if recorded.get(key) != value:
            raise ValueError(f'Release manifest mismatch: {key}; repackage before uploading.')
    expected = os.environ.get('RETTY_EXPECTED_SHA256')
    if expected and actual['ipaSha256'] != expected:
        raise ValueError('The IPA changed since this workflow checkpoint. Start a new release run.')
    return recorded


def package():
    c = config()
    build = number(os.environ['RETTY_BUILD_NUMBER'])
    if c['version'] != os.environ['RETTY_VERSION']:
        raise ValueError('The marketing version changed after resolving release metadata.')
    env = {**os.environ, 'GOWORK': 'off'}
    for key in ('IOS_PROVISIONING_PROFILE', 'IOS_SIGNING_IDENTITY', 'MYGO_INSPECTOR'):
        env.pop(key, None)
    invoke(['./scripts/check-mygo.sh'], stdout=sys.stderr, env=env)
    OUT.mkdir(parents=True, exist_ok=True)
    with (OUT / 'Build.log').open('w') as log:
        process = subprocess.Popen(['./scripts/build-ios.sh', '-o', str(OUT),
                                    '-ios-team', c['ios']['developmentTeam'],
                                    '-ios-export-method', 'app-store-connect', '-ios-build-number', build],
                                   cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        for line in process.stdout:
            sys.stderr.write(line)
            log.write(line)
            log.flush()
        if process.wait():
            raise ValueError('Packaging failed. Previous completed artifacts remain in the fixed directory; see Build.log.')
    release = verify()
    if release['buildNumber'] != build:
        raise ValueError('Exported build number differs from the requested build number.')
    release['mygo'] = invoke(['go', 'list', '-m', '-f', '{{.Replace.Version}}', 'github.com/egoist/mygo'],
                             stdout=subprocess.PIPE, text=True, env=env).stdout.strip()
    release['ascVersion'] = json.loads((ROOT / '.asc/toolchain.json').read_text())['version']
    release['sourceCommit'] = invoke(['git', 'rev-parse', 'HEAD'], stdout=subprocess.PIPE, text=True).stdout.strip()
    release['sourceDirty'] = bool(invoke(['git', 'status', '--porcelain'], stdout=subprocess.PIPE, text=True).stdout.strip())
    save(MANIFEST, release)
    return release


def upload():
    release = check()
    app_id = resolve_app()
    if RECEIPT.is_file():
        previous = json.loads(RECEIPT.read_text())
        if previous.get('ipaSha256') == release['ipaSha256'] and previous.get('appId') == app_id:
            return previous
    receipt = asc('builds', 'upload', '--app', app_id, '--ipa', str(IPA), '--checksum')
    result = {**release, 'appId': app_id, 'receipt': receipt}
    save(RECEIPT, result)
    return result


def wait():
    release = check()
    result = asc('builds', 'wait', '--app', os.environ['RETTY_APP_ID'], '--version', release['version'],
                 '--build-number', release['buildNumber'], '--platform', 'IOS', '--fail-on-invalid')
    if not re.fullmatch(r'[A-Za-z0-9_-]+', str(result.get('buildId', ''))):
        raise ValueError('Processing result is missing a valid buildId; inspect the saved upload with status.')
    return result


def require_group():
    if not os.environ.get('TESTFLIGHT_GROUP', '').strip():
        raise ValueError('TESTFLIGHT_GROUP is required; no default group is assigned.')
    return {'group': os.environ['TESTFLIGHT_GROUP']}


def distribute():
    require_group()
    check()
    return asc('builds', 'add-groups', '--build-id', os.environ['RETTY_BUILD_ID'],
               '--group', os.environ['TESTFLIGHT_GROUP'])


def status():
    release = check()
    return asc('builds', 'info', '--app', resolve_app(), '--version', release['version'],
               '--build-number', release['buildNumber'], '--platform', 'IOS')


def run(args):
    if not args or args == ['--help'] or args == ['-h']:
        print('Usage: scripts/release-ios.sh <package|check|upload|testflight|status> [KEY:VALUE ...] [--dry-run|--resume RUN_ID]\n'
              'Example: scripts/release-ios.sh package BUILD_NUMBER:3\n'
              'Artifacts: build/app-store/ios-arm64/; release manifest: build/app-store/Release.json')
        return
    lock_path = ROOT / '.mygo/ios/release.lock'
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    with lock_path.open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError('Another Retty iOS release workflow is running.')
        invoke(['./scripts/asc.sh', 'workflow', 'run', '--file', '.asc/workflow.json', *args])


if __name__ == '__main__':
    os.chdir(ROOT)
    try:
        if len(sys.argv) > 1 and sys.argv[1] == 'run':
            run(sys.argv[2:])
        else:
            operations = {'context': context, 'package': package, 'check': check, 'upload': upload,
                          'wait': wait, 'require-group': require_group, 'distribute': distribute, 'status': status}
            print(json.dumps(operations[sys.argv[1]](), ensure_ascii=False))
    except (ValueError, KeyError, subprocess.CalledProcessError) as error:
        print(f'ios-release: {error}', file=sys.stderr)
        sys.exit(1)
