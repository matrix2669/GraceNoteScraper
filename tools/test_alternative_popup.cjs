let suiteCompleted=false,suiteFailed=false;
const completionWatchdog=setTimeout(()=>{console.error('ALTERNATIVE_POPUP_INCOMPLETE: watchdog expired');process.exit(1)},10000);completionWatchdog.unref();
process.on('beforeExit',()=>{if(!suiteCompleted&&!suiteFailed){console.error('ALTERNATIVE_POPUP_INCOMPLETE: exited before final assertions');process.exitCode=1}});
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const script = fs.readFileSync('lineuparr.html','utf8').split('<script>')[1].split('</script>')[0];
new vm.Script(script);
function fn(name,next) {return script.slice(script.indexOf(`    async function ${name}(`),script.indexOf(`    function ${next}(`));}
const helpers=require('./review_fixture_helpers.cjs')(script)+script.slice(script.indexOf('    function saveMatchDecision('),script.indexOf('    async function decideMatch('));
const alternate=helpers+fn('decideAlternative','closeMatchAlternatives');
const decisionStart=script.indexOf('    async function decideMatch(');
const decisionEnd=script.indexOf('    function openMatchAlternatives(',decisionStart);
assert.ok(decisionStart>=0 && decisionEnd>decisionStart,'decideMatch extraction boundaries must be ordered and present');
const decision=script.slice(decisionStart,decisionEnd);
assert.match(decision,/async function decideMatch[\s\S]*await saveMatchDecision/,'extraction contains the actual decision handler');
assert.doesNotMatch(decision,/offerMatchAlternatives\s*\(/,'successful primary choices no longer open the alternatives popup');
assert.doesNotMatch(decision,/Other qualifying possibilities|close this list/,'popup-specific success copy is removed');
(async()=>{
 for(const decision of ['confirmed','denied']) for(const count of [1,2]) for(const fail of [false,true]) {
  let release; const saving=new Promise(r=>release=r); let closed=0,refills=0,removed=0;
  const target={key:'b',streamCount:2,reviewKind:'inclusion'}, survivor={key:'c',alternatives:[target]};
  const primary={alias:'Network',alternatives:count===1?[target]:[target,survivor]};
  const c={draft:{sourceFingerprint:'fixture'},pendingDecisions:new Set(),dispatchBusy:false,builderResetPending:false,decisionSaveQueue:Promise.resolve(),matchReconcileTimer:null,clearTimeout(){},setTimeout(){},setDispatchControls(){},decisionRevision:0,dispatchReviewEpoch:0,
   renderGuidedWorkflow(){},workflowRemote:{},scheduleWorkflowRefresh(){},
   matchReview:{candidates:[target,survivor],candidateCount:2,candidateStreamCount:4,confirmedCount:0,deniedCount:0,decisions:[]},
   api:async()=>{await saving;if(fail)throw Error('save failed');return {saved:true};},renderMatchReview(){},reloadDraft:async()=>{},showMessage(){},
   adjustReviewKindCount(){},refillResolvedPage:async()=>{refills++;},closeMatchAlternatives(){closed++;},
   els:{alternativeDialog:{dataset:{reviewKey:'primary'}},alternativeList:{querySelector:()=>count>1}},
   primary,target,decision,card:{querySelectorAll:()=>[],remove:()=>{removed++;}}};
  const pending=vm.runInNewContext(alternate+';decideAlternative(primary,target,decision,card)',c);await Promise.resolve();
  assert.equal(closed,0);assert.equal(removed,0);release();await pending;
  assert.equal(closed,!fail&&count===1?1:0);assert.equal(removed,fail?0:1);
  assert.equal(c.matchReview.candidateCount,fail?2:1);assert.equal(refills,fail?0:1);
 }
 // A response for an obsolete review cannot remove cards or close a newer popup.
 let release;const c={matchReview:{operationScope:'scope',decisionTokens:{b:'op-b'}},draft:{sourceFingerprint:'fixture'},pendingDecisions:new Set(),dispatchBusy:false,builderResetPending:false,decisionSaveQueue:Promise.resolve(),matchReconcileTimer:null,clearTimeout(){},setTimeout(){},setDispatchControls(){},decisionRevision:0,dispatchReviewEpoch:0,
 renderGuidedWorkflow(){},renderMatchReview(){},workflowRemote:{},scheduleWorkflowRefresh(){},api:()=>new Promise(r=>release=r),
 els:{alternativeDialog:{dataset:{reviewKey:'old'}}},primary:{},target:{key:'b'},decision:'denied',card:{querySelectorAll:()=>[],remove(){throw Error('stale removal');}},refillResolvedPage:async()=>{}};
 const p=vm.runInNewContext(alternate+';decideAlternative(primary,target,decision,card)',c);await Promise.resolve();c.dispatchReviewEpoch++;release({saved:true});await p;
 suiteCompleted=true;clearTimeout(completionWatchdog);
 console.log('ALTERNATIVE_POPUP_COMPLETE: Alternative popup: pending/failure, last item, multiple items, Confirm/Deny and stale replies pass');
})().catch(e=>{suiteFailed=true;clearTimeout(completionWatchdog);console.error(e);process.exitCode=1;});
