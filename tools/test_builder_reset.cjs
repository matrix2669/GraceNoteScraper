const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const script=fs.readFileSync('lineuparr.html','utf8').split('<script>')[1].split('</script>')[0];
const code=script.slice(script.indexOf('    let builderResetBackup'),script.indexOf('    function setProviderAddressStatus'));
(async()=>{
 for(const confirm of [false,true]) {
  let posts=0,reloads=0;const buttons={};
  const c={draft:{},dispatchBusy:false,saving:false,pendingDecisions:new Set(),document:{getElementById:id=>buttons[id]??={}},confirm:()=>confirm,
   invalidateDispatchReview(){},closeMatchAlternatives(){},setDispatchControls(){},scheduleWorkflowRefresh(){},showMessage(){},matchReview:{},matchReviewLimit:200,matchInputSignature:'old',
   reloadDraft:async()=>{reloads++},loadMatchReview:async()=>{},
   api:async(path,options)=>{if(options?.method==='POST'){posts++;const b=JSON.parse(options.body);assert.equal(b.confirm,true);return {backup:'opaque-backup',generation:1}}return {sourceFingerprint:'source',token:'current',clears:['builder choices'],preserves:['guide','TMDB']};}};
  await vm.runInNewContext(code+';resetBuilder()',c);
  assert.equal(posts,confirm?1:0);assert.equal(reloads,confirm?1:0);assert.equal(c.dispatchBusy,false);
  if(confirm){assert.equal(c.matchReview,null);assert.equal(c.matchReviewLimit,100);assert.equal(c.matchInputSignature,null)}
 }
 console.log('Builder reset UI: precise confirmation, cancel no-op, explicit reset and local snapshot invalidation pass');
})().catch(e=>{console.error(e);process.exitCode=1});
