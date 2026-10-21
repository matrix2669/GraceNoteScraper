const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const script = fs.readFileSync('lineuparr.html','utf8').split('<script>')[1].split('</script>')[0];
new vm.Script(script);
function fn(name,next) {return script.slice(script.indexOf(`    async function ${name}(`),script.indexOf(`    function ${next}(`));}
const alternate=fn('decideAlternative','closeMatchAlternatives');
const line=script.split('\n').find(x=>x.includes('const alternatives = candidate.minimumScore'));
for(const minimumScore of [94,95,98]) {const c={candidate:{minimumScore,alternatives:[{}]}};vm.runInNewContext(line+';result=alternatives.length;',c);assert.equal(c.result,minimumScore<95?1:0);}
(async()=>{
 for(const decision of ['confirmed','denied']) for(const count of [1,2]) for(const fail of [false,true]) {
  let release; const saving=new Promise(r=>release=r); let closed=0,refills=0,removed=0;
  const target={key:'b',streamCount:2,reviewKind:'inclusion'}, survivor={key:'c',alternatives:[target]};
  const primary={alias:'Network',alternatives:count===1?[target]:[target,survivor]};
  const c={pendingDecisions:new Set(),dispatchBusy:false,decisionRevision:0,dispatchReviewEpoch:0,
   renderGuidedWorkflow(){},workflowRemote:{},scheduleWorkflowRefresh(){},
   matchReview:{candidates:[target,survivor],candidateCount:2,candidateStreamCount:4,confirmedCount:0,deniedCount:0,decisions:[]},
   api:async()=>{await saving;if(fail)throw Error('save failed');},renderMatchReview(){},reloadDraft:async()=>{},showMessage(){},
   adjustReviewKindCount(){},refillResolvedPage:async()=>{refills++;},closeMatchAlternatives(){closed++;},
   els:{alternativeDialog:{dataset:{reviewKey:'primary'}},alternativeList:{querySelector:()=>count>1}},
   primary,target,decision,card:{querySelectorAll:()=>[],remove:()=>{removed++;}}};
  const pending=vm.runInNewContext(alternate+';decideAlternative(primary,target,decision,card)',c);
  assert.equal(closed,0);assert.equal(removed,0);release();await pending;
  assert.equal(closed,!fail&&count===1?1:0);assert.equal(removed,fail?0:1);
  assert.equal(c.matchReview.candidateCount,fail?2:1);assert.equal(refills,fail?0:1);
 }
 // A response for an obsolete review cannot remove cards or close a newer popup.
 let release;const c={pendingDecisions:new Set(),dispatchBusy:false,decisionRevision:0,dispatchReviewEpoch:0,
 renderGuidedWorkflow(){},workflowRemote:{},scheduleWorkflowRefresh(){},api:()=>new Promise(r=>release=r),
 els:{alternativeDialog:{dataset:{reviewKey:'old'}}},primary:{},target:{key:'b'},decision:'denied',card:{querySelectorAll:()=>[],remove(){throw Error('stale removal');}},refillResolvedPage:async()=>{}};
 const p=vm.runInNewContext(alternate+';decideAlternative(primary,target,decision,card)',c);c.dispatchReviewEpoch++;release();await p;
 console.log('Alternative popup: pending/failure, last item, multiple items, Confirm/Deny and stale replies pass');
})().catch(e=>{console.error(e);process.exitCode=1;});
