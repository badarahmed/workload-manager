import copy
import json
import tempfile
import unittest
from lifecycle import execute

class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.request = dict(container_id='a'*64, container_name='d3', image='sha256:123', operation='op1', running=True)
        self.item = dict(Id='a'*64, Name='/d3', Image='sha256:123', State=dict(Running=False, StartedAt='old-start'))
        self.starts = 0
        self.stops = 0
        self.lose_reply = False
        self.lose_stop_reply = False
    def docker(self, *args):
        if args[0]=='inspect': return json.dumps([self.item])
        if args[0]=='start':
            self.starts += 1; self.item['State']['Running']=True
            self.item['State']['StartedAt']='new-start'
            if self.lose_reply: raise ConnectionError('Reply lost after start')
            return self.item['Id']
        if args[0]=='stop':
            self.stops += 1; self.item['State']['Running']=False
            if self.lose_stop_reply: raise ConnectionError('Reply lost after stop')
            return self.item['Id']
        raise AssertionError(args)
    def test_lost_reply_does_not_duplicate_start(self):
        self.lose_reply=True
        with self.assertRaises(ConnectionError): execute(self.request,self.docker,self.directory.name)
        self.lose_reply=False
        result=execute(self.request,self.docker,self.directory.name)
        self.assertEqual(self.starts,1)
        self.assertTrue(result['recovered'])
        self.assertFalse(result['changed'])
    def test_identity_drift_blocks_mutation(self):
        self.item['Image']='sha256:other'
        with self.assertRaises(ValueError): execute(self.request,self.docker,self.directory.name)
        self.assertEqual(self.starts,0)
    def test_operation_cannot_change_intent(self):
        execute(self.request,self.docker,self.directory.name)
        changed=copy.copy(self.request); changed['running']=False
        with self.assertRaises(ValueError): execute(changed,self.docker,self.directory.name)
    def test_restart_lost_start_reply_does_not_stop_again(self):
        self.request['action']='restart'; self.item['State']['Running']=True
        self.lose_reply=True
        with self.assertRaises(ConnectionError): execute(self.request,self.docker,self.directory.name)
        self.lose_reply=False
        result=execute(self.request,self.docker,self.directory.name)
        self.assertEqual((self.stops,self.starts),(1,1))
        self.assertEqual(result['phase'],'verified')
        execute(self.request,self.docker,self.directory.name)
        self.assertEqual((self.stops,self.starts),(1,1))
    def test_restart_lost_stop_reply_resumes_start(self):
        self.request['action']='restart'; self.item['State']['Running']=True
        self.lose_stop_reply=True
        with self.assertRaises(ConnectionError): execute(self.request,self.docker,self.directory.name)
        self.lose_stop_reply=False
        execute(self.request,self.docker,self.directory.name)
        self.assertEqual((self.stops,self.starts),(1,1))
    def test_completed_restart_is_not_repeated_after_later_crash(self):
        self.request['action']='restart'; self.item['State']['Running']=True
        execute(self.request,self.docker,self.directory.name)
        self.item['State']['Running']=False
        result=execute(self.request,self.docker,self.directory.name)
        self.assertFalse(result['running'])
        self.assertEqual((self.stops,self.starts),(1,1))

    def test_unsuccessful_action_returns_failed_versioned_receipt(self):
        def docker(*args):
            if args[0] == 'inspect': return json.dumps([self.item])
            return ''  # Simulate start accepted but process immediately exited.
        result = execute(self.request, docker, self.directory.name)
        self.assertEqual(result['version'], 1)
        self.assertEqual(result['phase'], 'failed')

if __name__=='__main__': unittest.main()
