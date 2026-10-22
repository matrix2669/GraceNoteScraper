const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const script=fs.readFileSync('lineuparr.html','utf8').split('<script>')[1].split('</script>')[0];
const refill=script.slice(script.indexOf('    async function refillResolvedPage('),script.indexOf('    function matchCard('));
(async()=>{
 for(const test of [
  {candidates:[],candidateCount:5,requiredCount:5,want:1},
  {candidates:[{reviewKind:'qualified'}],candidateCount:5,requiredCount:0,want:0},
  {candidates:[{reviewKind:'inclusion'}],candidateCount:5,requiredCount:5,want:0},
  {candidates:[],candidateCount:0,requiredCount:0,want:0},
  {candidates:[{reviewKind:'qualified'}],candidateCount:1,requiredCount:0,want:0},
 ]) {
  let calls=0;const c={matchReview:test,dispatchBusy:false,pendingDecisions:new Set(),pendingAlternativeReviews:[],els:{alternativeDialog:{hasAttribute:()=>false}},matchReviewLimit:100,loadMatchReview:async(force,interactive,limit)=>{assert.equal(force,false);assert.equal(interactive,true);assert.equal(limit,100);calls++;}};
  await vm.runInNewContext(refill+';refillResolvedPage()',c);assert.equal(calls,test.want);
 }
 const more=script.slice(script.indexOf('    async function loadMoreMatches('),script.indexOf('    async function refillResolvedPage('));
 let release,calls=0;const c={matchReview:{candidateCount:400},dispatchBusy:false,pendingDecisions:new Set(),pendingAlternativeReviews:[],matchReviewLimit:100,loadMatchReview:async()=>{c.dispatchBusy=true;calls++;await new Promise(r=>release=r);c.dispatchBusy=false;}};
 const first=vm.runInNewContext(more+';loadMoreMatches()',c);await vm.runInNewContext('loadMoreMatches()',c);assert.equal(calls,1);release();await first;
 console.log('Automatic refill and single-flight manual fallback pass (all visible scores, empty/exhausted, rapid clicks)');
})().catch(e=>{console.error(e);process.exitCode=1;});
