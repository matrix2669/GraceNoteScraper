const fs=require('node:fs'),assert=require('node:assert/strict');
const {spawnSync}=require('node:child_process');
if(!process.env.REVIEW_HANDLER_URL){
 for(const hz of ['10','0']){
  const run=spawnSync('go',['test','.','-run','^TestReviewLifecycleBrowserIntegration$','-count=1','-timeout=90s','-v'],{encoding:'utf8',env:{...process.env,GOCACHE:process.env.GOCACHE||'/tmp/gracenote-audit-gocache',REVIEW_BROWSER_NODE:process.execPath,REVIEW_BURST_HZ:hz},timeout:100000});
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
    section('    function setDispatchControls(', '    function showConfirmedMatches(')+
    section('    let draftRequest = 0;', '    function renderAll(');
   await page.evaluate(({code,fixture,hz})=>{
    window.els={};for(const [key,id]of Object.entries({matchReview:'match-review',matchStreams:'match-streams',matchCandidates:'match-candidates',matchConfirmed:'match-confirmed',matchConfirmedJump:'match-confirmed-jump',matchDenied:'match-denied',matchList:'match-list',dispatchLoading:'dispatch-loading',matchStatus:'match-status',decisionList:'decision-list',decisionHistory:'decision-history',alternativeDialog:'match-alternative-dialog',alternativeList:'match-alternative-list',alternativeTitle:'match-alternative-title',alternativeHelp:'match-alternative-help',exportOpen:'export-open',dispatchURL:'dispatch-url',dispatchAuth:'dispatch-auth',dispatchUser:'dispatch-user',dispatchPassword:'dispatch-password',dispatchAPIKey:'dispatch-api-key',dispatchSave:'dispatch-save',dispatchRefresh:'dispatch-refresh',dispatchDisconnect:'dispatch-disconnect'}))els[key]=document.getElementById(id);
    Object.assign(window,{draft:fixture.draft,matchReview:fixture.review,dispatchConfig:{configured:true},publishing:false,saving:false,pendingChannelSaves:0,pendingDecisions:new Set(),dispatchBusy:false,decisionRevision:0,dispatchReviewEpoch:0,matchReviewRequest:0,dispatchBusyRequest:0,workflowRemote:{},matchReviewLimit:100,decisionSaveQueue:Promise.resolve(),matchReconcileTimer:null,pendingAlternativeReviews:[],builderResetPending:false});
    for(const name of ['renderGuidedWorkflow','scheduleWorkflowRefresh','renderAll','refreshResetRecovery'])window[name]=()=>{};
    window.messages=[];window.showMessage=(text,error)=>messages.push({text,error});window.emptyMessage=text=>{const p=document.createElement('p');p.textContent=text;return p};
    window.receipt={hz,accepted:0,posts:0,refills:0,acks:[],clickWork:[],start:performance.now(),lastClick:0,lastAck:0,nextPage:0,maxHeartbeat:0};
    const fetchReal=window.fetch.bind(window);window.fetch=async(path,options)=>{const at=performance.now();const response=await fetchReal(path,options);if(options?.method==='POST'){receipt.posts++;receipt.acks.push(performance.now()-at);receipt.lastAck=performance.now();if(!response.ok)throw Error(`decision HTTP ${response.status}`)}else if(String(path).includes('/dispatcharr/review')){receipt.refills++;}return response};
    (0,eval)(code+'\nwindow.renderMatchReview=renderMatchReview;');renderMatchReview();
    window.keys=matchReview.candidates.map(c=>c.key);if(keys.length!==100||keys.some(k=>!k))throw Error('expected100 admitted fixture groups');
    let lastBeat=performance.now();window.beat=setInterval(()=>{const now=performance.now();receipt.maxHeartbeat=Math.max(receipt.maxHeartbeat,now-lastBeat);lastBeat=now;},25);
    const observer=new MutationObserver(()=>{if(receipt.accepted===100&&pendingDecisions.size===0&&document.querySelector('#match-list>.match-card')){receipt.nextPage=performance.now();observer.disconnect();clearInterval(beat);}});observer.observe(els.matchList,{childList:true});
    window.burst=(async()=>{for(let i=0;i<keys.length;i++){const key=keys[i];const card=[...document.querySelectorAll('#match-list>.match-card')].find(c=>c.dataset.key===key);const button=card?.querySelector(i%2?'.deny-button':'.confirm-button');if(!button||button.disabled)throw Error(`click ${i} unexpectedly blocked`);const at=performance.now();receipt.lastClick=at;button.click();receipt.clickWork.push(performance.now()-at);receipt.accepted++;if(document.querySelector(`[data-key="${key}"]`)?.classList.contains('match-card'))throw Error('click not accepted');if(hz)await new Promise(r=>setTimeout(r,1000/hz));}})();
   },{code,fixture,hz:Number(process.env.REVIEW_BURST_HZ||0)});
   await page.evaluate(()=>burst);
   await page.waitForFunction(()=>receipt.nextPage>0,{},{timeout:15000});
   const metrics=await page.evaluate(()=>({...receipt,dimensions:{streams:matchReview.streamCount,remaining:matchReview.candidateCount,visible:matchReview.candidates.length},errors:messages.filter(m=>m.error).length,states:[...matchActionState.states.values()].map(s=>s.state)}));
   assert.equal(metrics.posts,100);assert.equal(metrics.refills,1);assert.equal(metrics.errors,0);assert.ok(metrics.states.every(s=>s==='saved'));
   assert.ok(Math.max(...metrics.clickWork)<250,'immediate acceptance within250ms');
   const nextMS=metrics.nextPage-metrics.lastClick;if(metrics.hz===10)assert.ok(nextMS<3000,`last click to next page ${nextMS.toFixed(1)}ms`);
   metrics.lastClickToNextPageMS=nextMS;metrics.lastClickToFinalAckMS=metrics.lastAck-metrics.lastClick;metrics.p95AckMS=[...metrics.acks].sort((a,b)=>a-b)[94];
   delete metrics.states;
   if(process.env.REVIEW_RETAINED_ROOT)fs.writeFileSync(`${process.env.REVIEW_RETAINED_ROOT}/browser-handler-${metrics.hz}hz.json`,JSON.stringify(metrics,null,2),{mode:0o600});
   console.log(`Real browser→Go handler: 100 at ${metrics.hz||'rapid'}Hz, p95 HTTP acknowledgement ${metrics.p95AckMS.toFixed(1)}ms, final click→next page ${nextMS.toFixed(1)}ms, exactly one refill`);
  }finally{await browser.close()}
 })().catch(e=>{console.error(e);process.exitCode=1});
}
