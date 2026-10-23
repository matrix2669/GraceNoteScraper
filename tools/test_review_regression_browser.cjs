const fs=require('node:fs'),assert=require('node:assert/strict'),{spawnSync}=require('node:child_process');
if(!process.env.REVIEW_REGRESSION_URL){
 const r=spawnSync('go',['test','.','-run','^TestReviewRegressionBrowser$','-count=1','-timeout=90s','-v'],{encoding:'utf8',env:{...process.env,REVIEW_BROWSER_NODE:process.execPath},timeout:100000});
 process.stdout.write(r.stdout||'');process.stderr.write(r.stderr||'');process.exitCode=r.status||0;
}else{
 const {chromium}=require('playwright');
 (async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});try{
  const page=await browser.newPage(),origin=process.env.REVIEW_REGRESSION_URL,errors=[];
  page.on('pageerror',e=>errors.push(e.message));await page.route('**/*',r=>r.request().url().startsWith(origin+'/')?r.continue():r.abort());
  await page.goto(origin);const fixture=await page.evaluate(async()=>{const r=await fetch('/fixture');return r.json()});
  const html=fs.readFileSync('lineuparr.html','utf8'),part=(a,b)=>html.slice(html.indexOf(a),html.indexOf(b,html.indexOf(a)));
  await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
  const code=part('    async function api(','    let builderResetBackup')+part('    async function loadMatchReview(','    function showConfirmedMatches(')+part('    let draftRequest = 0;','    function renderAll(');
  await page.evaluate(({code,fixture})=>{
   window.els={};for(const [key,id]of Object.entries({matchReview:'match-review',matchStreams:'match-streams',matchCandidates:'match-candidates',matchConfirmed:'match-confirmed',matchConfirmedJump:'match-confirmed-jump',matchDenied:'match-denied',matchList:'match-list',dispatchLoading:'dispatch-loading',matchStatus:'match-status',decisionList:'decision-list',decisionHistory:'decision-history',alternativeDialog:'match-alternative-dialog',alternativeList:'match-alternative-list',alternativeTitle:'match-alternative-title',alternativeHelp:'match-alternative-help',exportOpen:'export-open',dispatchURL:'dispatch-url',dispatchAuth:'dispatch-auth',dispatchUser:'dispatch-user',dispatchPassword:'dispatch-password',dispatchAPIKey:'dispatch-api-key',dispatchSave:'dispatch-save',dispatchRefresh:'dispatch-refresh',dispatchDisconnect:'dispatch-disconnect'}))els[key]=document.getElementById(id);
   Object.assign(window,{draft:fixture.draft,matchReview:fixture.review,dispatchConfig:{configured:true},publishing:false,saving:false,pendingChannelSaves:0,pendingDecisions:new Set(),dispatchBusy:false,decisionRevision:0,dispatchReviewEpoch:0,matchReviewRequest:0,dispatchBusyRequest:0,workflowRemote:{},matchReviewLimit:100,decisionSaveQueue:Promise.resolve(),matchReconcileTimer:null,pendingAlternativeReviews:[],builderResetPending:false});
   for(const name of ['renderGuidedWorkflow','scheduleWorkflowRefresh','renderAll','refreshResetRecovery'])window[name]=()=>{};
   window.messages=[];window.showMessage=(text,error)=>messages.push({text,error});window.emptyMessage=text=>{const p=document.createElement('p');p.textContent=text;return p};
   const realFetch=window.fetch.bind(window);window.fetch=async(path,options)=>{
    if(options?.method==='POST'&&String(path).includes('/dispatcharr/decision')){
     if(window.rejectNext){window.rejectNext=false;options={...options,body:JSON.stringify({...JSON.parse(options.body),decision:'invalid'})};}
     const response=await realFetch(path,options);if(window.loseAck){window.loseAck=false;throw Error('injected lost acknowledgement after real durable save')};return response;
    }return realFetch(path,options);
   };
   (0,eval)(code+'\nwindow.renderMatchReview=renderMatchReview;');window.matchReview=mergeSupplementalReview(fixture.review);renderMatchReview();
   window.unknown=matchReview.candidates.find(c=>c.reviewWarnings?.length);window.explicit=matchReview.candidates.find(c=>!c.reviewWarnings?.length);
   if(!unknown||!explicit)throw Error('expected explicit and unresolved feed groups');
  },{code,fixture});
  const unknown=await page.evaluate(()=>window.unknown.key);
  const unknownCard=page.locator(`article[data-key="${unknown}"]`);
  assert.match(await unknownCard.innerText(),/902 · HBO Hits \(HBOHTHD\)/);
  assert.match(await unknownCard.innerText(),/Exact normalized alias: HBO/);
  await unknownCard.locator('summary').filter({hasText:'Manual feed review required'}).click();
  assert.match(await unknownCard.innerText(),/USER-ATTESTED exact station\/feed: user-attested\/2026-10-22#59368:east/);
  assert.match(await page.locator('#match-status').innerText(),/Primary: 2 pending.*supplemental: 0 pending.*Total: 2/);
  await page.evaluate(async()=>{window.rejectNext=true;await decideMatch(unknown.key,'confirmed')});
  assert.equal(await page.evaluate(()=>matchActionState(unknown.key).state),'failed');
  assert.equal(await page.evaluate(()=>matchReview.confirmedCount),0);
  assert.equal(await page.evaluate(()=>matchReview.candidateCount),2);
  assert.match(await page.locator('#match-status').innerText(),/Primary: 2 pending/);
  assert.match(await page.locator(`article[data-key="${unknown}"]`).innerText(),/Save not acknowledged/);
  await page.evaluate(async()=>{await decideMatch(unknown.key,'confirmed');await loadMatchReview(false,true)});
  assert.equal(await page.evaluate(()=>matchReview.confirmedCount),1);
  assert.equal(await page.evaluate(()=>matchReview.decisions[0].streamCount),2);
  assert.equal(await page.evaluate(()=>matchReview.decisions[0].reason),'Lineuparr alias');
  await page.evaluate(async()=>{window.loseAck=true;await decideMatch(explicit.key,'denied')});
  assert.equal(await page.evaluate(()=>matchActionState(explicit.key).state),'uncertain');
  await page.evaluate(async()=>{await recoverMatchActions()});
  assert.equal(await page.evaluate(()=>matchActionState(explicit.key).state),'saved');
  assert.equal(await page.evaluate(()=>matchReview.deniedCount),1);
  assert.equal(await page.evaluate(()=>matchReview.candidateCount),0);
  await page.evaluate(async()=>{const old=matchActionState(explicit.key).operation;const saved=matchReview.decisions.find(d=>d.decision==='denied');await clearDecision(saved.key);const r=await fetch('/api/lineuparr/dispatcharr/decision',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({key:explicit.key,operation:old,decision:'denied'})});if(r.status!==409)throw Error('delayed retry undid Undo');});
  assert.equal(await page.evaluate(()=>matchReview.deniedCount),0);
  assert.equal(await page.evaluate(()=>matchReview.candidateCount),1);
  assert.deepEqual(errors,[]);
  console.log('Real browser→HTTP regression passed: readable current target, exact lookup label, full215byte provenance, validated400 rollback, retry/group counts, lost-ack receipt, Undo and delayed-token fence.');
 }finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
}
