const fs=require('node:fs'),assert=require('node:assert/strict'),{spawnSync}=require('node:child_process');
if(!process.env.UNIFIED_REPAIR_URL){
 const r=spawnSync('go',['test','.','-run','^TestUnifiedRepairBrowserIntegration$','-count=1','-timeout=90s','-v'],{encoding:'utf8',env:{...process.env,REVIEW_BROWSER_NODE:process.execPath},timeout:100000});
 process.stdout.write(r.stdout||'');process.stderr.write(r.stderr||'');process.exitCode=r.status||0;
}else{
 const {chromium}=require('playwright');
 (async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});try{
  for(const prior of process.env.UNIFIED_REPAIR_PRIOR?[process.env.UNIFIED_REPAIR_PRIOR]:['idle','complete']){
   const page=await browser.newPage(),origin=process.env.UNIFIED_REPAIR_URL,errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.route('**/*',r=>r.request().url().startsWith(origin+'/')?r.continue():r.abort());await page.goto(origin);
   const fixture=await page.evaluate(async()=>{const r=await fetch('/fixture');if(!r.ok)throw Error(await r.text());return r.json()});
   const html=fs.readFileSync('lineuparr.html','utf8'),part=(a,b)=>html.slice(html.indexOf(a),html.indexOf(b,html.indexOf(a)));
   const code=part('    async function api(','    let builderResetBackup')+part('    async function loadMatchReview(','    function showConfirmedMatches(')+part('    function sameArray(',"    document.addEventListener('DOMContentLoaded'")+part('    async function generateLineup(','    els.exportOpen.addEventListener');
   await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
   await page.evaluate(({code,fixture,prior})=>{
    window.els={};for(const [key,id]of Object.entries({matchReview:'match-review',matchStreams:'match-streams',matchCandidates:'match-candidates',matchConfirmed:'match-confirmed',matchConfirmedJump:'match-confirmed-jump',matchDenied:'match-denied',matchList:'match-list',dispatchLoading:'dispatch-loading',matchStatus:'match-status',decisionList:'decision-list',decisionHistory:'decision-history',alternativeDialog:'match-alternative-dialog',alternativeList:'match-alternative-list',alternativeTitle:'match-alternative-title',alternativeHelp:'match-alternative-help',exportOpen:'export-open',dispatchURL:'dispatch-url',dispatchAuth:'dispatch-auth',dispatchUser:'dispatch-user',dispatchPassword:'dispatch-password',dispatchAPIKey:'dispatch-api-key',dispatchSave:'dispatch-save',dispatchRefresh:'dispatch-refresh',dispatchDisconnect:'dispatch-disconnect'}))els[key]=document.getElementById(id);
    Object.assign(window,{draft:fixture.draft,dispatchConfig:{configured:true},publishing:false,saving:false,pendingChannelSaves:0,pendingDecisions:new Set(),dispatchBusy:false,decisionRevision:0,dispatchReviewEpoch:0,matchReviewRequest:0,dispatchBusyRequest:0,workflowRemote:{},matchReviewLimit:100,decisionSaveQueue:Promise.resolve(),matchReconcileTimer:null,pendingAlternativeReviews:[],builderResetPending:false,localExportChanges:new Set()});
    for(const name of ['renderGuidedWorkflow','scheduleWorkflowRefresh','renderSummary','setDispatchControls'])window[name]=()=>{};
    window.reloadDraft=async()=>{};window.refreshExportSummary=async()=>{};window.messages=[];window.showMessage=(text,error)=>messages.push({text,error});window.emptyMessage=text=>{const p=document.createElement('p');p.textContent=text;return p};
    (0,eval)(code+'\nwindow.renderMatchReview=renderMatchReview;window.mergeSupplementalReview=mergeSupplementalReview;window.generateLineup=generateLineup;window.setImpactOwner=()=>matchReviewImpactOwner=publicationViewSnapshot();');
    const all=mergeSupplementalReview(fixture.review),candidate=all.candidates.find(c=>c.channelId===fixture.choiceTarget&&c.constituents?.some(p=>p.m3uAccountId===3&&p.streamId===2));if(!candidate)throw Error('missing durable choice fixture');
    window.choice=candidate.key;window.matchReview={...all,candidates:[candidate],candidateCount:1,candidateStreamCount:candidate.streamCount,supplementalImpact:{state:prior,rows:[]}};
    const extra=n=>({...candidate,key:`extra-${n}`,alias:`New extra ${n}`,streamNames:[`New extra ${n}`]});window.extraRows=[extra(1),extra(2)];
    window.saveAtServer=false;window.pollsWhilePending=0;window.afterAckPolls=0;window.refills=0;window.published=0;window.reviewReads=0;
    const realAPI=api;
    window.api=async(path,options)=>{
     if(path.includes('/decision')&&options?.method==='POST'){
      const result=await realAPI(path,options);saveAtServer=true;await new Promise(resolve=>window.releaseAck=resolve);return result;
     }
     if(path.includes('supplemental-impact')){
      if(pendingDecisions.size){pollsWhilePending++;return {state:prior,rows:[],unresolved:false};}
      afterAckPolls++;return afterAckPolls===1?{state:'running',rows:[]}:{state:prior,rows:[],unresolved:false};
     }
     if(path.includes('/review?')){
      reviewReads++;return {...fixture.review,reviewItems:extraRows,reviewedItems:[],candidateCount:2,candidateStreamCount:2,supplementalImpact:{state:prior,rows:[]}};
     }
     if(path.includes('/publish'))published++;
     return realAPI(path,options);
    };
    const load=loadMatchReview;window.loadMatchReview=async(...args)=>{refills++;return load(...args)};
    setImpactOwner();renderMatchReview();window.beforeChoice=matchReview.decisions.length;
   },{code,fixture,prior});
   await page.locator('#match-list .confirm-button').first().click();
   await page.waitForFunction(()=>saveAtServer&&pollsWhilePending>0);
   assert.equal(await page.evaluate(()=>pendingDecisions.size),1);
   assert.equal(await page.evaluate(()=>matchReview.supplementalImpact.state),'queued','previous terminal result overwrote pending revision');
   assert.equal(await page.evaluate(()=>matchReview.decisions.length),await page.evaluate(()=>beforeChoice+1),'optimistic saved row was replaced');
   assert.equal(await page.evaluate(()=>refills),0);
   await page.evaluate(()=>releaseAck());
   await page.waitForFunction(()=>pendingDecisions.size===0&&document.querySelectorAll('#match-list>.match-card').length===2,{},{timeout:8000});
   assert.equal(await page.evaluate(()=>refills),1,'expected one refill after new union count');
   assert.equal(await page.evaluate(()=>matchReview.candidateCount),2);
   assert.equal(await page.evaluate(()=>matchReview.candidates[0].key),'extra-1');
   assert.ok(await page.evaluate(()=>afterAckPolls>=2),'replacement was not observed');
   await page.evaluate(async()=>{await generateLineup()});
   const notification=await page.evaluate(()=>messages.at(-1));assert.equal(notification.error,undefined);assert.match(notification.text,/Lineup file generated/);assert.match(notification.text,/validated automatically in the background/);assert.match(notification.text,/account outcomes/);assert.match(notification.text,/future inventory/);assert.doesNotMatch(notification.text,/Check supplemental export impact|after a batch|unchecked/i);
   assert.equal(await page.evaluate(()=>published),1,'real generation handler not invoked');assert.deepEqual(errors,[]);
   console.log(`Prior ${prior}: real durable save held across terminal status, optimistic row preserved, replacement observed,2extras visible with1refill; real generation notification uses automatic validation`);
   await page.close();
  }
 }finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
}
