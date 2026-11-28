const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const html=fs.readFileSync('lineuparr.html','utf8');
const code=html.slice(html.indexOf('    let draftRequest ='),html.indexOf('    function renderAll('));
(async()=>{
 let replies=[],renders=0;
 const c={draft:{name:'original'},dispatchReviewEpoch:1,decisionRevision:0,api:()=>new Promise(r=>replies.push(r)),renderAll:()=>renders++,refreshResetRecovery:async()=>{}};
 vm.runInNewContext(code,c);
 const old=c.reloadDraft(1);c.dispatchReviewEpoch=2;
 const current=c.reloadDraft(2);replies[1]({name:'current'});await current;replies[0]({name:'obsolete'});await old;
 assert.equal(c.draft.name,'current');assert.equal(renders,1);
 const stale=c.reloadDraft(2);c.dispatchReviewEpoch=3;replies[2]({name:'reset-old'});assert.equal(await stale,false);
 assert.equal(c.draft.name,'current');
 const duringSave=c.reloadDraft(3);c.decisionRevision++;replies[3]({name:'pre-decision'});assert.equal(await duringSave,false);assert.equal(c.draft.name,'current');
 console.log('Draft reload: late request and obsolete decision epochs cannot overwrite current state');
})().catch(e=>{console.error(e);process.exitCode=1});
