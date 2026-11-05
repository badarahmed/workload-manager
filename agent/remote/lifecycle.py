"""Bounded Docker lifecycle step. Executed via SSH/python stdin, never a daemon."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys


def execute(request, docker=None, root=None):
    if docker is None:
        def docker(*args):
            return subprocess.check_output(['docker', *args], text=True, timeout=55)
    required = ('container_id', 'container_name', 'image', 'operation', 'running')
    if any(k not in request for k in required):
        raise ValueError('Incomplete lifecycle request')
    if not re.fullmatch(r'[a-f0-9]{64}', request['container_id']):
        raise ValueError('Exact container ID required')
    if not re.fullmatch(r'[a-zA-Z0-9_-]{1,128}', request['operation']):
        raise ValueError('Invalid operation identity')
    if not isinstance(request['running'], bool):
        raise ValueError('running must be boolean')
    restart = request.get('action') == 'restart'
    if request.get('action', 'set_running') not in ('set_running', 'restart') or (restart and not request['running']):
        raise ValueError('Invalid lifecycle action')
    root = Path(root or Path.home() / '.local/state/wm')
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    # This host-wide lock is held by the remote process, not the SSH client.
    with (root / 'docker-mutation.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        def inspect():
            item = json.loads(docker('inspect', request['container_id']))[0]
            if item['Id'] != request['container_id'] or item['Name'] != '/' + request['container_name'] or item['Image'] != request['image']:
                raise ValueError('Runtime identity changed; replan required')
            return item
        item = inspect()
        receipt = root / (request['operation'] + '.json')
        previous = json.loads(receipt.read_text()) if receipt.exists() else None
        identity = hashlib.sha256(json.dumps(request, sort_keys=True).encode()).hexdigest()
        if previous and previous['request_hash'] != identity:
            raise ValueError('Operation identity reused for a different request')
        def save(phase, **fields):
            data = dict(version=1, request_hash=identity, phase=phase, container_id=item['Id'], **fields)
            temporary = receipt.with_suffix('.tmp')
            with temporary.open('w') as out:
                os.chmod(temporary, 0o600)
                json.dump(data, out); out.flush(); os.fsync(out.fileno())
            os.replace(temporary, receipt)
            fd = os.open(root, os.O_RDONLY)
            try: os.fsync(fd)
            finally: os.close(fd)
            return data
        if restart:
            # One restart per operation key. Preserve phases across lost replies;
            # never stop a newly started process just to replay the tool call.
            if previous and previous['phase'] == 'verified':
                return dict(previous, version=1, recovered=True, running=item['State']['Running'])
            initial = previous.get('initial_started_at') if previous else item['State'].get('StartedAt')
            if not initial:
                raise ValueError('Restart requires a captured start timestamp')
            phase = previous['phase'] if previous else 'restart_intent'
            if phase == 'restart_intent':
                if item['State']['Running'] and item['State'].get('StartedAt') != initial:
                    return save('verified', changed=True, recovered=True, initial_started_at=initial)
                save('restart_intent', initial_started_at=initial)
                if item['State']['Running']:
                    docker('stop', '--time', '30', item['Id'])
                item = inspect()
                if item['State']['Running']:
                    raise RuntimeError('Stop outcome unresolved; inspect before retry')
                save('restart_starting', initial_started_at=initial)
            item = inspect()
            if not item['State']['Running']:
                docker('start', item['Id'])
            item = inspect()
            if not item['State']['Running']:
                raise RuntimeError('Restart did not reach running state')
            return save('verified', changed=True, recovered=bool(previous), initial_started_at=initial)
        if item['State']['Running'] == request['running']:
            return save('verified', changed=False, recovered=bool(previous))
        save('intent')
        # If the client disconnects after this action, a retry inspects the same ID.
        if request['running']:
            docker('start', item['Id'])
        else:
            docker('stop', '--time', '30', item['Id'])
        item = inspect()
        if item['State']['Running'] != request['running']:
            return save('failed', changed=True, reason='Runtime did not reach requested process state')
        return save('verified', changed=True, recovered=bool(previous))


if __name__ == '__main__':
    try:
        result = execute(json.load(sys.stdin))
        print(json.dumps(result))
        if result['phase'] != 'verified': sys.exit(1)
    except Exception as error:
        print(json.dumps({'version': 1, 'phase': 'rejected' if isinstance(error, ValueError) else 'unknown', 'error': str(error)}))
        sys.exit(1)
