const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const html=fs.readFileSync('lineuparr.html','utf8');
const section=(a,b)=>html.slice(html.indexOf(a),html.indexOf(b,html.indexOf(a)));
const helper=require('./review_fixture_helpers.cjs')(html);
(async()=>{
 const c={draft:{sourceFingerprint:'fixture'},dispatchBusy:false,builderResetPending:false,pendingDecisions:new Set(),decisionSaveQueue:Promise.resolve(),dispatchReviewEpoch:0,renderMatchReview(){},api:async()=>({saved:true})};vm.createContext(c);
 vm.runInContext(helper+section('    function saveMatchDecision(','    let reviewOrder'),c);
 let release;c.api=()=>new Promise(r=>release=r);const first=c.saveMatchDecision('one','confirmed',0);await Promise.resolve();assert.equal(c.matchActionState('one').state,'saving');const second=c.saveMatchDecision('two','denied',0);assert.equal(c.matchActionState('two').state,'queued');c.dispatchReviewEpoch++;release({saved:true});await first;await assert.rejects(second,/before this choice was sent/);assert.equal(c.matchActionState('one').state,'saved');assert.equal(c.matchActionState('two').state,'cancelled');
 c.api=async()=>{const e=Error('acknowledgement lost');e.uncertain=true;throw e};await assert.rejects(c.saveMatchDecision('lost','confirmed',1));assert.equal(c.matchActionState('lost').state,'uncertain');assert.match(c.reviewActionBlock('lost','denied'),/Retry the same choice/);assert.equal(c.reviewActionBlock('lost','confirmed'),'');
 c.reconcileMatchActionStates({decisions:[{key:'lost',decision:'confirmed'}]});assert.equal(c.matchActionState('lost').state,'saved');
 c.api=async()=>{throw Error('write failed')};await assert.rejects(c.saveMatchDecision('bad','denied',1));assert.equal(c.matchActionState('bad').state,'failed');
 c.pendingDecisions.add('pending');assert.match(c.reviewActionBlock('pending','confirmed'),/already queued/);assert.match(c.reviewActionBlock('','',true),/Waiting for/);assert.equal(c.reviewActionBlock('other','confirmed'),'');c.pendingDecisions.clear();c.builderResetPending=true;assert.match(c.reviewActionBlock('other','confirmed'),/reset/);c.builderResetPending=false;c.dispatchBusy=true;assert.match(c.reviewActionBlock(),/in progress/);
 // A failed follow-up read cannot turn a durable Undo into a failed write.
 const u={...c,dispatchBusy:false,matchReview:{},decisionRevision:0,workflowRemote:{},showMessage(text){u.message=text},setDispatchControls(){},renderGuidedWorkflow(){},api:async()=>({saved:true}),reloadDraft:async()=>{throw Error('read offline')},loadMatchReview:async()=>{}};vm.createContext(u);vm.runInContext(helper+section('    async function clearDecision(','    function setDispatchControls('),u);await u.clearDecision('saved');assert.match(u.message,/Match review undone.*Could not update/);assert.equal(u.dispatchBusy,false);
 console.log('Action feedback: queued/saving/saved/failed/cancelled/uncertain, safe retry, guard reasons and durable Undo/read failure pass');
})().catch(e=>{console.error(e);process.exitCode=1});
