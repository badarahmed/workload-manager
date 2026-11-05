import json
import os
from pathlib import Path
import select
import signal
import subprocess
import sys
import tempfile
import unittest

from tofu_verbose import arguments


class VerboseTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        fake = self.root / 'tofu'
        fake.write_text('''#!/usr/bin/env python3
import json,os,sys,time,signal
from pathlib import Path
path=os.environ.get('WM_PROGRESS_FILE')
print(json.dumps({'args':sys.argv[1:],'progress':bool(path),'agentConfig':os.environ.get('WM_AGENT_CONFIG')}),flush=True)
if path:
 with Path(path).open('a') as stream:
  stream.write(json.dumps({'workload':'d3','message':'Starting captured container'})+'\\n')
  stream.flush()
if os.environ.get('FIXTURE_WARNING'):
 print('Warning: Provider development overrides are in effect\\nDetails\\nreleases.',flush=True)
 print('Error: fixture failure',file=sys.stderr,flush=True)
mode=os.environ.get('FIXTURE_MODE','normal')
if mode=='signal':
 signal.signal(signal.SIGINT,lambda *_:sys.exit(130))
 print('READY',flush=True)
 time.sleep(30)
if mode=='live':time.sleep(1)
sys.exit(int(os.environ.get('FIXTURE_EXIT','0')))
''')
        fake.chmod(0o700)
        self.env = dict(os.environ, PATH=str(self.root) + ':' + os.environ['PATH'])
        self.command = [sys.executable, str(Path(__file__).with_name('tofu_verbose.py')), str(self.root)]

    def run_wrapper(self, *args, **env):
        return subprocess.run(self.command + list(args), env=dict(self.env, **env), capture_output=True, text=True, timeout=5)

    def test_flags_and_argument_passthrough(self):
        self.assertEqual(arguments(['apply', '--verbose', 'plan with spaces']), (True, ['apply', 'plan with spaces']))
        self.assertEqual(arguments(['--verbose', 'apply', '--', '--verbose']), (True, ['apply', '--', '--verbose']))
        result = self.run_wrapper('apply', '--verbose', 'plan with spaces', FIXTURE_EXIT='2')
        self.assertEqual(result.returncode, 2)
        self.assertEqual(json.loads(result.stdout)['args'], ['-chdir=' + str(self.root), 'apply', 'plan with spaces'])
        self.assertIn('[wm:d3] Starting captured container', result.stderr)

    def test_default_is_quiet_and_does_not_inherit_old_progress_sink(self):
        result = self.run_wrapper('validate', WM_PROGRESS_FILE='/unused')
        self.assertEqual(result.returncode, 0)
        self.assertFalse(json.loads(result.stdout)['progress'])
        self.assertEqual(result.stderr, '')

    def test_progress_is_visible_before_command_finishes(self):
        child = subprocess.Popen(self.command + ['apply', '--verbose'], env=dict(self.env, FIXTURE_MODE='live'), stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            readable, _, _ = select.select([child.stderr], [], [], 3)
            self.assertTrue(readable)
            self.assertIn('Starting captured container', child.stderr.readline())
            self.assertIsNone(child.poll())
            child.communicate(timeout=5)
            self.assertEqual(child.returncode, 0)
        finally:
            if child.poll() is None: child.kill()
            child.communicate()

    def test_warning_filter_preserves_failures_and_verbose_progress(self):
        for extra in ([], ['--verbose']):
            result = self.run_wrapper('apply', *extra, WM_HIDE_DEV_WARNING='1', FIXTURE_WARNING='1', FIXTURE_EXIT='7')
            self.assertEqual(result.returncode, 7)
            self.assertNotIn('development overrides', result.stdout)
            self.assertIn('Error: fixture failure', result.stderr)
            if extra:
                self.assertIn('[wm:d3]', result.stderr)

    def test_agent_config_discovery_and_explicit_override(self):
        (self.root / 'agent.json').write_text('{}')
        (self.root / 'agent.yaml').write_text('version: 1')
        env = self.env.copy()
        env.pop('WM_AGENT_CONFIG', None)
        result = subprocess.run(self.command + ['validate'], env=env, capture_output=True, text=True, timeout=5)
        self.assertEqual(json.loads(result.stdout)['agentConfig'], str((self.root / 'agent.yaml').resolve()))
        overridden = self.run_wrapper('validate', WM_AGENT_CONFIG='/explicit/agent.json')
        self.assertEqual(json.loads(overridden.stdout)['agentConfig'], '/explicit/agent.json')

    def test_interrupt_is_forwarded_and_exit_status_preserved(self):
        child = subprocess.Popen(self.command + ['apply', '--verbose'], env=dict(self.env, FIXTURE_MODE='signal'), stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            self.assertTrue(select.select([child.stdout], [], [], 3)[0])
            child.stdout.readline()
            self.assertEqual(child.stdout.readline().strip(), 'READY')
            child.send_signal(signal.SIGINT)
            child.communicate(timeout=5)
            self.assertEqual(child.returncode, 130)
        finally:
            if child.poll() is None: child.kill()
            child.communicate()



class ScopeTests(unittest.TestCase):
    def test_full_and_scoped_commands_use_the_same_wrapper(self):
        from tofu_verbose import scoped_arguments
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / '.terraform/modules').mkdir(parents=True)
            modules = []
            for folder, key in [('apps/langfuse', 'apps.langfuse'), ('models/d3', 'models.d3')]:
                (root / folder).mkdir(parents=True)
                modules.append({'Key': key, 'Dir': folder})
            modules.append({'Key': 'langfuse', 'Dir': 'apps/langfuse'})  # stale migration entry
            (root / '.terraform/modules/modules.json').write_text(json.dumps({'Modules': modules}))
            for action in ('plan', 'apply'):
                self.assertEqual(scoped_arguments(root, [action, '--scope', '']), [action])
                self.assertEqual(scoped_arguments(root, [action]), [action])
                for scope in ('apps/langfuse', 'models/d3'):
                    expected = '-target=module.' + scope.replace('/', '.module.')
                    self.assertEqual(scoped_arguments(root, [action, '--scope', scope]), [action, expected])
            for args in (['plan', '--scope'], ['plan', '--scope', '../escape'],
                         ['plan', '--scope', 'apps/missing'], ['destroy', '--scope', 'apps/langfuse']):
                with self.assertRaises(ValueError):
                    scoped_arguments(root, args)
            self.assertEqual(scoped_arguments(root, ['apply', '--', '--scope', 'literal']), ['apply', '--', '--scope', 'literal'])

if __name__ == '__main__':
    unittest.main()
