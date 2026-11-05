import {test} from 'node:test';
import assert from 'node:assert/strict';
import {codexCredential} from './auth.js';

test('Codex bridge reads access token only and rejects expired/malformed credentials',()=>{
 const token=(exp:number)=>'header.'+Buffer.from(JSON.stringify({exp})).toString('base64url')+'.signature';
 const access=token(10000);
 const result=codexCredential(JSON.stringify({tokens:{access_token:access,refresh_token:'must-not-copy'}}),0);
 assert.equal(result.access,access);assert.equal(result.refresh,'');
 assert.equal(JSON.stringify(result).includes('must-not-copy'),false);
 assert.throws(()=>codexCredential(JSON.stringify({tokens:{access_token:token(1)}}),1000),/expires too soon/);
 assert.throws(()=>codexCredential('invalid-secret-document'),e=>e instanceof Error&&!e.message.includes('invalid-secret-document'));
});
