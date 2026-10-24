const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
let completed=false,failed=false;
const probe=process.env.ACTION_FEEDBACK_INCOMPLETE_PROBE==='1';
const keepAliveProbe=process.env.ACTION_FEEDBACK_KEEP_ALIVE_PROBE==='1';
const watchdog=setTimeout(()=>{console.error('ACTION_FEEDBACK_INCOMPLETE: watchdog expired before final assertions');process.exit(1)},keepAliveProbe?200:10000);watchdog.unref();
process.on('beforeExit',()=>{if(!completed&&!failed){console.error('ACTION_FEEDBACK_INCOMPLETE: beforeExit reached without final assertions');process.exitCode=1}});
const html=fs.readFileSync('lineuparr.html','utf8');
const section=(a,b)=>html.slice(html.indexOf(a),html.indexOf(b,html.indexOf(a)));
const helper=require('./review_fixture_helpers.cjs')(html);
(async()=>{
 if(probe||keepAliveProbe){if(keepAliveProbe)setInterval(()=>{},50);await new Promise(()=>{})}
 const milestones=[];
 const c={draft:{sourceFingerprint:'fixture'},dispatchBusy:false,builderResetPending:false,pendingDecisions:new Set(),decisionSaveQueue:Promise.resolve(),dispatchReviewEpoch:0,matchReview:{operationScope:'scope',decisionTokens:{one:'op-one',two:'op-two',lost:'op-lost',bad:'op-bad'}},showMessage(){},renderMatchReview(){},api:async()=>({saved:true})};vm.createContext(c);
 vm.runInContext(helper+section('    function saveMatchDecision(','    let reviewOrder'),c);
 let release;c.api=()=>new Promise(r=>release=r);
 const first=c.saveMatchDecision('one','confirmed',0);await Promise.resolve();assert.equal(c.matchActionState('one').state,'saving');
 const second=c.saveMatchDecision('two','denied',0);assert.equal(c.matchActionState('two').state,'queued');
 c.dispatchReviewEpoch++;release({saved:true});await first;await assert.rejects(second,/before this choice was sent/);
 assert.equal(c.matchActionState('one').state,'saved');assert.equal(c.matchActionState('two').state,'cancelled');milestones.push('sent-and-unsent-retirement');
 c.api=async()=>{const e=Error('acknowledgement lost');e.uncertain=true;throw e};
 const lost=c.saveMatchDecision('lost','confirmed',1);await new Promise(r=>setImmediate(r));assert.equal(c.matchActionState('lost').state,'uncertain');
 assert.match(c.reviewActionBlock('lost','denied'),/Retry the same choice/);assert.equal(c.reviewActionBlock('lost','confirmed'),'');
 c.api=async()=>({operation:'op-lost',key:'lost',decision:'confirmed',state:'saved'});await c.reconcileMatchActionStates({operationScope:'scope',decisions:[],candidates:[]});await lost;assert.equal(c.matchActionState('lost').state,'saved');milestones.push('uncertain-receipt-recovery');
 // These are synthetic non-job review states. They must not retain a completed
 // transmitted job from the prior lifecycle merely because its display key is reused.
 c.matchActionState('lost','uncertain',{operation:'old-op',decision:'confirmed',scope:'scope',source:'fixture',job:null});
 let receiptRelease;c.api=()=>new Promise(r=>receiptRelease=r);
 const checking=c.reconcileMatchActionStates({operationScope:'scope'});
 c.matchActionState('lost','uncertain',{operation:'new-op',job:null});
 receiptRelease({operation:'old-op',key:'lost',decision:'confirmed',state:'saved'});await checking;
 assert.equal(c.matchActionState('lost').state,'uncertain');assert.equal(c.matchActionState('lost').operation,'new-op');milestones.push('newer-operation-fence');
 c.api=async()=>{throw Error('superseded synthetic state must not request a receipt')};
 await c.reconcileMatchActionStates({operationScope:'replacement-snapshot'});assert.equal(c.matchActionState('lost').state,'cancelled');milestones.push('replacement-snapshot');
 c.matchActionState('lost','uncertain',{operation:'source-op',source:'old-source',job:null});await c.reconcileMatchActionStates({operationScope:'scope'});assert.equal(c.matchActionState('lost').state,'cancelled');milestones.push('source-replacement');
 c.api=async()=>{throw Error('write failed')};await assert.rejects(c.saveMatchDecision('bad','denied',1));assert.equal(c.matchActionState('bad').state,'failed');milestones.push('definite-write-failure');
 c.pendingDecisions.add('pending');assert.match(c.reviewActionBlock('pending','confirmed'),/already queued/);assert.match(c.reviewActionBlock('','',true),/Waiting for/);assert.equal(c.reviewActionBlock('other','confirmed'),'');c.pendingDecisions.clear();
 c.builderResetPending=true;assert.match(c.reviewActionBlock('other','confirmed'),/reset/);c.builderResetPending=false;c.dispatchBusy=true;assert.match(c.reviewActionBlock(),/in progress/);milestones.push('guard-reasons');
 const u={...c,dispatchBusy:false,matchReview:{},decisionRevision:0,workflowRemote:{},showMessage(text){u.message=text},setDispatchControls(){},renderGuidedWorkflow(){},api:async()=>({saved:true}),reloadDraft:async()=>{throw Error('read offline')},loadMatchReview:async()=>{}};
 vm.createContext(u);vm.runInContext(helper+section('    async function clearDecision(','    function setDispatchControls('),u);await u.clearDecision('saved');assert.match(u.message,/Match review undone.*Could not update/);assert.equal(u.dispatchBusy,false);milestones.push('durable-undo-read-failure');
 const recovery={...c,dispatchBusy:false,pendingDecisions:new Set(),showMessage(text){recovery.message=text},reloadDraft:async()=>{throw Error('draft unavailable')},loadMatchReview:async()=>{},setDispatchControls(){},renderGuidedWorkflow(){}};
 vm.createContext(recovery);vm.runInContext(helper,recovery);recovery.renderMatchRecovery=()=>{};
 recovery.matchActionState('receipt','uncertain',{operation:'receipt-op',decision:'confirmed',scope:'scope',source:'fixture',job:null});recovery.api=async()=>({operation:'receipt-op',key:'receipt',decision:'confirmed',state:'saved'});
 await recovery.recoverMatchActions();assert.equal(recovery.matchActionState('receipt').state,'saved');assert.match(recovery.message,/Save state checked.*Could not update/);assert.equal(recovery.recoverMatchActions.pending,false);milestones.push('durable-recovery-read-failure');
 assert.deepEqual(milestones,['sent-and-unsent-retirement','uncertain-receipt-recovery','newer-operation-fence','replacement-snapshot','source-replacement','definite-write-failure','guard-reasons','durable-undo-read-failure','durable-recovery-read-failure']);
 completed=true;clearTimeout(watchdog);
 console.log('ACTION_FEEDBACK_COMPLETE '+JSON.stringify({assertionGroups:milestones.length,milestones}));
})().catch(error=>{failed=true;clearTimeout(watchdog);console.error(error);process.exitCode=1});
