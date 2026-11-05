#!/usr/bin/env python3
"""OpenTofu passthrough with an invocation-scoped wm progress stream."""
import json
import os
from pathlib import Path
import signal
import re
import subprocess
import sys
import tempfile
import threading
from warnings_filter import filter_stream


def arguments(args):
    forwarded = []
    verbose = False
    positional = False
    for arg in args:
        if arg == '--':
            positional = True
        if arg == '--verbose' and not positional:
            verbose = True
        else:
            forwarded.append(arg)
    return verbose, forwarded


def scoped_arguments(root, args):
    """Resolve the wrapper's optional scope before calling native OpenTofu."""
    forwarded = []
    scope = None
    positional = False
    index = 0
    while index < len(args):
        arg = args[index]
        if arg == '--':
            positional = True
        if arg == '--scope' and not positional:
            if scope is not None or index + 1 >= len(args):
                raise ValueError('--scope requires one workload folder')
            scope = args[index + 1]
            index += 2
            continue
        forwarded.append(arg)
        index += 1
    if scope is None or scope == '':
        return forwarded
    if not forwarded or forwarded[0] not in ('plan', 'apply'):
        raise ValueError('--scope is supported only for plan and apply')
    if not re.fullmatch(r'(apps|models)/[a-zA-Z0-9_-]+', scope):
        raise ValueError('Expected a workload path such as apps/langfuse or models/d3')
    root = Path(root)
    directory = root / scope
    if not directory.is_dir():
        raise ValueError('Workload directory does not exist: ' + scope)
    try:
        modules = json.loads((root / '.terraform/modules/modules.json').read_text())['Modules']
        matches = [m['Key'] for m in modules
                   if m.get('Key', '').startswith(scope.split('/')[0] + '.')
                   and (root / m['Dir']).resolve() == directory.resolve()]
    except (OSError, ValueError, KeyError, TypeError):
        raise ValueError('Module inventory unavailable; run ./tofu get first') from None
    if len(matches) != 1 or not re.fullmatch(r'[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]+)*', matches[0]):
        raise ValueError('Workload must have one registered module; run ./tofu get after editing module declarations')
    position = forwarded.index('--') if '--' in forwarded else len(forwarded)
    forwarded.insert(position, '-target=module.' + matches[0].replace('.', '.module.'))
    return forwarded


def follow(path, stop, output):
    # Writer uses one append per small JSON record. Hold incomplete records until
    # their newline arrives; multiple resources may report concurrently.
    with path.open() as stream:
        pending = ''
        while True:
            pending += stream.read()
            while '\n' in pending:
                line, pending = pending.split('\n', 1)
                try:
                    event = json.loads(line)
                    label = event['workload']
                    message = event['message']
                    if not isinstance(label, str) or not isinstance(message, str):
                        continue
                    # Strip terminal control sequences even for malformed writers.
                    clean = lambda value: ''.join(c for c in value if c.isprintable())
                    output.write(f"[wm:{clean(label)[:80]}] {clean(message)[:1000]}\n")
                    output.flush()
                except (ValueError, KeyError, TypeError):
                    continue
            if stop.is_set():
                return
            stop.wait(0.1)


def main():
    root, *args = sys.argv[1:]
    verbose, args = arguments(args)
    try:
        args = scoped_arguments(root, args)
    except ValueError as error:
        sys.exit(str(error))
    command = ['tofu', '-chdir=' + root, *args]
    env = os.environ.copy()
    env.pop('WM_PROGRESS_FILE', None)
    if 'WM_AGENT_CONFIG' not in env:
        for name in ('agent.yaml', 'agent.json'):
            config = Path(root) / name
            if config.is_file():
                env['WM_AGENT_CONFIG'] = str(config.resolve())
                break
    quiet_dev_warning = env.pop('WM_HIDE_DEV_WARNING', '') == '1'
    if not verbose and not quiet_dev_warning:
        os.execvpe(command[0], command, env)
    with tempfile.TemporaryDirectory(prefix='wm-progress-') as directory:
        path = Path(directory) / 'events.jsonl'
        path.touch(mode=0o600)
        if verbose:
            env['WM_PROGRESS_FILE'] = str(path)
        stop = threading.Event()
        reader = threading.Thread(target=follow, args=(path, stop, sys.stderr), daemon=True)
        # A separate session lets the wrapper forward exactly one interrupt while
        # leaving stdin/stdout/stderr inherited for normal OpenTofu interaction.
        child = subprocess.Popen(command, env=env, start_new_session=True,
                                 stdout=subprocess.PIPE if quiet_dev_warning else None,
                                 stderr=subprocess.PIPE if quiet_dev_warning else None)
        filters = []
        if quiet_dev_warning:
            for stream, output in [(child.stdout, sys.stdout), (child.stderr, sys.stderr)]:
                thread = threading.Thread(target=filter_stream, args=(stream, output), daemon=True)
                thread.start()
                filters.append(thread)
        previous = {}
        def relay(signum, _frame):
            try:
                os.killpg(child.pid, signum)
            except ProcessLookupError:
                pass
        for signum in (signal.SIGINT, signal.SIGTERM):
            previous[signum] = signal.signal(signum, relay)
        reader.start()
        try:
            status = child.wait()
        finally:
            stop.set()
            reader.join()
            for thread in filters:
                thread.join()
            for signum, handler in previous.items():
                signal.signal(signum, handler)
        return status if status >= 0 else 128 - status


if __name__ == '__main__':
    sys.exit(main())
