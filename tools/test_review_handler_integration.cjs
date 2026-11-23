const fs=require('node:fs'),assert=require('node:assert/strict');
const {spawnSync}=require('node:child_process');
if(!process.env.REVIEW_HANDLER_URL){
 for(const hz of ['10','0','recovery']){
  const run=spawnSync('go',['test','.','-run',hz==='recovery'?'^TestReviewRecoveryBrowserIntegration$':'^TestReviewLifecycleBrowserIntegration$','-count=1','-timeout=90s','-v'],{encoding:'utf8',env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/gracenote-audit-gocache',REVIEW_BROWSER_NODE:process.execPath,REVIEW_BURST_HZ:hz},timeout:100000});
  process.stdout.write(run.stdout||'');process.stderr.write(run.stderr||'');if(run.status!==0)process.exit(run.status||1);
 }
}else{
 const {chromium}=require('playwright');
 (async()=>{
  const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
  try{
   const page=await browser.newPage();const origin=process.env.REVIEW_HANDLER_URL;
   await page.route('**/*',r=>r.request().url().startsWith(origin+'/')?r.continue():r.abort());
   await page.goto(origin);
   const fixture=await page.evaluate(async()=>{const r=await fetch('/fixture');if(!r.ok)throw Error('fixture failed');return r.json()});
   const html=fs.readFileSync('lineuparr.html','utf8');
   await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
   const section=(a,b)=>html.slice(html.indexOf(a),html.indexOf(b,html.indexOf(a)));
   const code=section('    async function api(', '    let builderResetBackup')+
    section('    async function loadMatchReview(', '    async function clearDecision(')+
    section('    async function clearDecision(', '    function setDispatchControls(')+
    section('    function setDispatchControls(', '    function showConfirmedMatches(')+
    section('    let draftRequest = 0;', '    function renderAll(');
   await page.evaluate(({code,fixture,hz})=>{
    window.els={};for(const [key,id]of Object.entries({matchReview:'match-review',matchStreams:'match-streams',matchCandidates:'match-candidates',matchConfirmed:'match-confirmed',matchConfirmedJump:'match-confirmed-jump',matchDenied:'match-denied',matchList:'match-list',dispatchLoading:'dispatch-loading',matchStatus:'match-status',decisionList:'decision-list',decisionHistory:'decision-history',alternativeDialog:'match-alternative-dialog',alternativeList:'match-alternative-list',alternativeTitle:'match-alternative-title',alternativeHelp:'match-alternative-help',exportOpen:'export-open',dispatchURL:'dispatch-url',dispatchAuth:'dispatch-auth',dispatchUser:'dispatch-user',dispatchPassword:'dispatch-password',dispatchAPIKey:'dispatch-api-key',dispatchSave:'dispatch-save',dispatchRefresh:'dispatch-refresh',dispatchDisconnect:'dispatch-disconnect'}))els[key]=document.getElementById(id);
    Object.assign(window,{draft:fixture.draft,matchReview:fixture.review,dispatchConfig:{configured:true},publishing:false,saving:false,pendingChannelSaves:0,pendingDecisions:new Set(),dispatchBusy:false,decisionRevision:0,dispatchReviewEpoch:0,matchReviewRequest:0,dispatchBusyRequest:0,workflowRemote:{},matchReviewLimit:100,decisionSaveQueue:Promise.resolve(),matchReconcileTimer:null,pendingAlternativeReviews:[],builderResetPending:false,localExportChanges:new Set()});
    for(const name of ['renderGuidedWorkflow','scheduleWorkflowRefresh','renderAll','refreshResetRecovery'])window[name]=()=>{};
    window.messages=[];window.showMessage=(text,error)=>messages.push({text,error});window.emptyMessage=text=>{const p=document.createElement('p');p.textContent=text;return p};
    window.receipt={hz,accepted:0,posts:0,refills:0,acks:[],clickWork:[],start:performance.now(),lastClick:0,lastAck:0,nextPage:0,maxHeartbeat:0};
    const fetchReal=window.fetch.bind(window);window.fetch=async(path,options)=>{const at=performance.now();if(window.dropBeforeDecision&&options?.method==='POST'&&String(path).includes('/dispatcharr/decision')){window.dropBeforeDecision=false;throw Error('Synthetic request lost before server arrival')}if(window.holdNextReceipt&&String(path).includes('/dispatcharr/decision?operation=')){window.holdNextReceipt=false;return new Response('Synthetic running receipt',{status:409})}const response=await fetchReal(path,options);if(window.dropNextDecisionAck&&options?.method==='POST'&&String(path).includes('/dispatcharr/decision')){window.dropNextDecisionAck=false;window.holdNextReceipt=true;throw Error('Synthetic acknowledgement lost after durable handler response')}if(options?.method==='POST'){receipt.posts++;receipt.acks.push(performance.now()-at);receipt.lastAck=performance.now();}else if(String(path).includes('/dispatcharr/review')){receipt.refills++;}return response};
    (0,eval)(code+'\nwindow.renderMatchReview=renderMatchReview;');renderMatchReview();
    window.loseChoice=async(key)=>{
     window.dropNextDecisionAck=true;window.lostChoice=decideMatch(key,'confirmed');
     while(matchActionState(key).state!=='uncertain'||window.holdNextReceipt)await new Promise(r=>setTimeout(r,5));
    };
    window.finishRecovery=async()=>{await recoverMatchActions();await lostChoice;await reloadDraft();await loadMatchReview(false,true);};
    window.keys=matchReview.candidates.map(c=>c.key);if(keys.length!==100||keys.some(k=>!k))throw Error('expected100 admitted fixture groups');
    let lastBeat=performance.now();window.beat=setInterval(()=>{const now=performance.now();receipt.maxHeartbeat=Math.max(receipt.maxHeartbeat,now-lastBeat);lastBeat=now;},25);
    const observer=new MutationObserver(()=>{if(receipt.accepted===100&&pendingDecisions.size===0&&document.querySelector('#match-list>.match-card')){receipt.nextPage=performance.now();observer.disconnect();clearInterval(beat);}});observer.observe(els.matchList,{childList:true});
    if(fixture.recovery){clearInterval(beat);observer.disconnect();window.burst=Promise.resolve();return;}
    window.burst=(async()=>{receipt.start=performance.now();for(let i=0;i<keys.length;i++){if(hz&&i)await new Promise(r=>setTimeout(r,Math.max(0,receipt.start+i*1000/hz-performance.now())));const key=keys[i];const card=[...document.querySelectorAll('#match-list>.match-card')].find(c=>c.dataset.key===key);const button=card?.querySelector(i%2?'.deny-button':'.confirm-button');if(!button||button.disabled)throw Error(`click ${i} unexpectedly blocked`);const at=performance.now();receipt.lastClick=at;button.click();receipt.clickWork.push(performance.now()-at);receipt.accepted++;if(document.querySelector(`[data-key="${key}"]`)?.classList.contains('match-card'))throw Error('click not accepted');}})();
   },{code,fixture,hz:Number(process.env.REVIEW_BURST_HZ||0)});
   if(fixture.recovery){
    await page.evaluate(async key=>{
     window.originalOperation=matchReview.decisionTokens[key];window.originalKey=key;
     await loseChoice(key);
     if(matchActionState(key).state!=='uncertain')throw Error('lost grouped save was not uncertain');
     // Replace the page with actual history, then remove both page rows to
     // prove recovery depends on the receipt and its persistent global panel.
     const review=await api('/api/lineuparr/dispatcharr/review?limit=1');
     if(review.decisions[0].streamCount!==2)throw Error('group constituents lost');
     window.undoKey=review.decisions[0].key;matchReview={...review,candidates:[],decisions:[]};renderMatchReview();
     if(!document.querySelector('#match-save-recovery button'))throw Error('off-page recovery disappeared');
    },fixture.recoveryKey);
    // Hold the first recovery draft response after the saved receipt. A saved
    // action and idle controls alone do not mean the recovery task has finished.
    await page.evaluate(()=>{
     const real=window.fetch;window.holdRecoveryDraft=true;window.recoveryDraftHeld=false;
     window.fetch=async(path,options)=>{
      const response=await real(path,options);
      if(holdRecoveryDraft&&String(path).includes('/lineuparr/draft')){
       holdRecoveryDraft=false;window.recoveryDraftHeld=true;
       await new Promise(resolve=>window.releaseRecoveryDraft=resolve);
      }
      if(options?.method==='DELETE'&&String(path).includes('/dispatcharr/decision'))window.undoResponseStatus=response.status;
      return response;
     };
    });
    await page.locator('#match-save-recovery button').click();
    await page.waitForFunction(()=>recoveryDraftHeld&&matchActionState(originalKey).state==='saved'&&!dispatchBusy);
    assert.equal(await page.evaluate(()=>pendingDecisions.size),0,'receipt settlement did not drain original logical job');
    await page.evaluate(()=>releaseRecoveryDraft());
    await page.waitForFunction(()=>!recoverMatchActions.pending&&matchActionState(originalKey).state==='saved'&&!dispatchBusy);
    await page.evaluate(()=>loadMatchReview(false,true));
    await page.evaluate(async()=>{
     await clearDecision(undoKey);
     if(window.undoResponseStatus!==200)throw Error('Undo did not send a successful DELETE');
     if(matchReview.decisions.some(d=>d.key===undoKey))throw Error('Undo retained group');
     const response=await fetch('/api/lineuparr/dispatcharr/decision',{method:'POST',headers:{'Content-Type':'application/json','X-Builder-Generation':String(draft.resetGeneration||0)},body:JSON.stringify({key:originalKey,operation:originalOperation,decision:'confirmed',tvgIds:[]})});
     if(response.status!==409)throw Error('old token reversed Undo after GET');
     await decideMatch(originalKey,'denied');
     if(matchActionState(originalKey).state!=='saved')throw Error('fresh choice after Undo failed');
     // A receipt that says not-saved retires the delayed operation, and a
     // normal review reload must expose a usable fresh explicit choice.
     const absent=matchReview.candidates[0].key;const absentToken=matchReview.decisionTokens[absent];window.dropBeforeDecision=true;await decideMatch(absent,'confirmed');
     await loadMatchReview(false,true);
     if(matchActionState(absent).state!=='failed'||matchReview.decisionTokens[absent]===absentToken)throw Error('not-saved recovery retained old admission');
     await decideMatch(absent,'denied');if(matchActionState(absent).state!=='saved')throw Error('fresh choice after not-saved receipt blocked');
     // Lose a second acknowledgement, supersede its snapshot, then recover.
     const other=matchReview.candidates[0].key;await loseChoice(other);
     await fetch('/fixture/refresh',{method:'POST'});await finishRecovery();
     if(matchActionState(other).state!=='cancelled'||hasUncertainMatchActions())throw Error('snapshot supersession stranded recovery');
     // Repeat using the actual reset endpoint in this disposable Go fixture.
     const third=matchReview.candidates[0].key;await loseChoice(third);
     const summary=await api('/api/lineuparr/reset');
     await api('/api/lineuparr/reset',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({sourceFingerprint:summary.sourceFingerprint,token:summary.token,confirm:true,withdrawPublished:false})});
     await finishRecovery();
     if(matchActionState(third).state!=='cancelled'||hasUncertainMatchActions())throw Error('reset supersession stranded recovery');
     if(draft.resetGeneration!==summary.generation+1)throw Error('recovery retained the pre-reset request generation');
    });
    console.log('Real grouped browser→Go recovery: lost acknowledgement, off-page receipt, raw group Undo, old-token rejection after GET, fresh choice, refresh/reset supersession passed');
    return;
   }
   await page.evaluate(()=>burst);
   await page.waitForFunction(()=>receipt.nextPage>0,{},{timeout:15000});
   const metrics=await page.evaluate(()=>({...receipt,dimensions:{streams:matchReview.streamCount,remaining:matchReview.candidateCount,visible:matchReview.candidates.length},errors:messages.filter(m=>m.error).length,states:[...matchActionState.states.values()].map(s=>s.state)}));
   assert.equal(metrics.posts,100);assert.equal(metrics.refills,1);assert.equal(metrics.errors,0);assert.ok(metrics.states.every(s=>s==='saved'));
   assert.ok(Math.max(...metrics.clickWork)<250,'immediate acceptance within250ms');
   const nextMS=metrics.nextPage-metrics.lastClick;if(metrics.hz===10)assert.ok(nextMS<3000,`last click to next page ${nextMS.toFixed(1)}ms`);
   metrics.lastClickToNextPageMS=nextMS;metrics.lastClickToFinalAckMS=metrics.lastAck-metrics.lastClick;metrics.p95AckMS=[...metrics.acks].sort((a,b)=>a-b)[94];
   if(metrics.hz===10)assert.ok(metrics.lastClick-metrics.start>=9850&&metrics.lastClick-metrics.start<10500,'100 fixed-deadline clicks delivered at10Hz');
   assert.ok(metrics.p95AckMS<100,`p95 HTTP acknowledgement ${metrics.p95AckMS.toFixed(1)}ms`);
   delete metrics.states;
   if(process.env.REVIEW_RETAINED_ROOT)fs.writeFileSync(`${process.env.REVIEW_RETAINED_ROOT}/browser-handler-${metrics.hz}hz.json`,JSON.stringify(metrics,null,2),{mode:0o600});
   console.log(`Real browser→Go handler: 100 at ${metrics.hz||'rapid'}Hz, p95 HTTP acknowledgement ${metrics.p95AckMS.toFixed(1)}ms, final click→next page ${nextMS.toFixed(1)}ms, exactly one refill`);
  }finally{await browser.close()}
 })().catch(e=>{console.error(e);process.exitCode=1});
}
