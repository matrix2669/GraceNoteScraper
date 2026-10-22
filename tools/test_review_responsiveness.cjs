const {chromium}=require('playwright');
const fs=require('node:fs'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
 try {
  const page=await browser.newPage();await page.route('**/*',r=>r.abort());
  const html=fs.readFileSync('lineuparr.html','utf8');
  await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
  const code=html.slice(html.indexOf('    function renderAutomaticExclusions('),html.indexOf('    async function clearDecision('))+html.slice(html.indexOf('    function setDispatchControls('),html.indexOf('    function showConfirmedMatches('))+html.slice(html.indexOf("    els.alternativeDialog.addEventListener('close'"),html.indexOf("    document.getElementById('builder-reset').addEventListener"));
  await page.evaluate(code=>{
   window.els={};for(const [key,id] of Object.entries({matchReview:'match-review',matchStreams:'match-streams',matchCandidates:'match-candidates',matchConfirmed:'match-confirmed',matchConfirmedJump:'match-confirmed-jump',matchDenied:'match-denied',matchList:'match-list',dispatchLoading:'dispatch-loading',matchStatus:'match-status',decisionList:'decision-list',decisionHistory:'decision-history',alternativeDialog:'match-alternative-dialog',alternativeList:'match-alternative-list',alternativeTitle:'match-alternative-title',alternativeHelp:'match-alternative-help'}))els[key]=document.getElementById(id);
   for(const [key,id] of Object.entries({exportOpen:'export-open',dispatchURL:'dispatch-url',dispatchAuth:'dispatch-auth',dispatchUser:'dispatch-user',dispatchPassword:'dispatch-password',dispatchAPIKey:'dispatch-api-key',dispatchSave:'dispatch-save',dispatchRefresh:'dispatch-refresh',dispatchDisconnect:'dispatch-disconnect'}))els[key]=document.getElementById(id);
   Object.assign(window,{draft:{sourceFingerprint:'fixture',channels:[]},publishing:false,saving:false,pendingChannelSaves:0,pendingDecisions:new Set(),dispatchBusy:false,decisionRevision:0,dispatchReviewEpoch:0,workflowRemote:{},matchReviewLimit:100,decisionSaveQueue:Promise.resolve(),matchReconcileTimer:null,pendingAlternativeReviews:[],builderResetPending:false});
   for(const name of ['renderGuidedWorkflow','scheduleWorkflowRefresh','renderSummary'])window[name]=()=>{};
   window.messages=[];window.showMessage=(text,error)=>messages.push({text,error});
   window.emptyMessage=text=>{const p=document.createElement('p');p.textContent=text;return p};
   window.saves=[];window.drafts=[];
   window.api=async(path,options)=>new Promise((resolve,reject)=>saves.push({body:JSON.parse(options.body),resolve,reject}));
   window.reloadDraft=async(epoch,revision)=>new Promise(resolve=>drafts.push({epoch,revision,resolve}));
   window.candidate=(key,score=100)=>({key,channelId:key,reviewKind:score<95?'inclusion':'qualified',alias:key,streamNames:[key],streamCount:1,minimumScore:score,maximumScore:score,channelNumber:'1',channelName:'Fixture',reasons:[],alternatives:[]});
   window.matchReview={streamCount:21208,candidateCount:100,confirmedCount:722,deniedCount:0,candidates:Array.from({length:100},(_,i)=>candidate('row'+i)),decisions:Array.from({length:722},(_,i)=>({key:'old'+i,decision:'confirmed',streamName:'Saved '+i,channelName:'Fixture',streamCount:1,score:100})),automatic:{rows:Array.from({length:800},(_,i)=>({key:'auto'+i,exported:true,streamName:'Synthetic '+i,channelNumber:'1',channelName:'Fixture',reason:'identity fixture'}))}};
   window.loads=0;window.loadMatchReview=async()=>{loads++;};
   (0,eval)(code+'\nwindow.renderMatchReview=renderMatchReview;window.decideMatch=decideMatch;window.decideAlternative=decideAlternative;window.openMatchAlternatives=openMatchAlternatives;');renderMatchReview();
  },code);
  if(process.env.REVIEW_COST_INPUT && process.env.REVIEW_COST_DRAFT) {
   const input=JSON.parse(fs.readFileSync(process.env.REVIEW_COST_INPUT,'utf8'));
   const output=JSON.parse(fs.readFileSync(process.env.REVIEW_COST_DRAFT,'utf8'));
   const automatic=[...output.derived.Rows.map(r=>({key:r.Candidate.key,channelId:r.Candidate.channelId,channelName:r.Candidate.channelName,channelNumber:r.Candidate.channelNumber,streamName:r.Candidate.streamName,accountId:r.Candidate.m3uAccountId,reason:r.Reason,pendingReview:r.PendingReview,conflict:r.Conflict,exported:!r.Conflict})),...output.derived.inventoryRows.map(r=>({key:r.Key,channelId:r.ChannelID,channelName:r.ChannelName,channelNumber:r.ChannelNumber,streamName:r.StreamName,accountId:r.AccountID,reason:r.Reason,conflict:r.Conflict,exported:!r.Conflict,inventoryOnly:true}))];
   // Whitelist safe renderer fields; never pass source settings, stream URLs,
   // credentials or unrelated retained records to the browser.
   const decisions=input.decisions.map(d=>({key:d.key,decision:d.decision,channelId:d.channelId,channelName:d.channelName,channelNumber:d.channelNumber,streamName:d.streamName,streamCount:1,score:d.score}));
   await page.evaluate(({automatic,decisions,count})=>{matchReview={...matchReview,automatic:{rows:automatic},decisions,streamCount:count};renderMatchReview();},{automatic,decisions,count:input.streams.length});
  }
  const dimensions=await page.evaluate(()=>({streams:matchReview.streamCount,rows:matchReview.candidates.length,decisions:matchReview.decisions.length,constraints:matchReview.automatic.rows.length}));
  const row=key=>page.locator(`#match-list > .match-card[data-key="${key}"]`);
  await row('row0').locator('.confirm-button').click();
  await page.waitForFunction(()=>saves.length===1);
  assert.equal(await row('row0').count(),0);
  const elapsed=await page.evaluate(()=>{const t=performance.now();document.querySelector('[data-key="row1"] .deny-button').click();return performance.now()-t;});
  assert.equal(await row('row1').count(),0,'Next item must accept the click BEFORE the first save/draft finishes');
  assert.ok(elapsed<250,`next click synchronous work ${elapsed}ms`);
  assert.equal(await page.evaluate(()=>pendingDecisions.size),2);
  assert.equal(await page.evaluate(()=>{const e={preventDefault(){this.prevented=true}};protectUnsavedReview(e);return e.prevented}),true);
  assert.equal(await row('row3').locator('.confirm-button').isEnabled(),true);
  assert.equal(await page.locator('#export-open').isDisabled(),true);
  assert.equal(await page.locator('#dispatch-refresh').isDisabled(),true);
  await page.evaluate(()=>{window.heartbeats=0;window.heartbeat=setInterval(()=>heartbeats++,25)});
  // Slow persistence must not lose rapid choices. A failed first save restores
  // only its own item, and cannot cancel the following independently chosen row.
  await page.evaluate(()=>new Promise(resolve=>setTimeout(()=>{saves[0].reject(Error('isolated write failure'));resolve();},5000)));
  await page.waitForFunction(()=>saves.length===2);
  assert.ok(await page.evaluate(()=>heartbeats)>100,'browser remains responsive throughout five-second save delay');await page.evaluate(()=>clearInterval(heartbeat));
  assert.equal(await row('row0').count(),1);assert.match(await row('row0').textContent(),/Save not acknowledged/);assert.equal(await row('row1').count(),0);
  await page.evaluate(()=>saves[1].resolve({saved:true}));
  await page.waitForFunction(()=>pendingDecisions.size===0);
  await page.waitForFunction(()=>drafts.length===1);
  await row('row2').locator('.confirm-button').click();
  await page.waitForFunction(()=>saves.length===3);
  assert.equal(await row('row2').count(),0,'background draft cannot lock next choice');
  await page.evaluate(()=>{drafts[0].resolve(false);saves[2].resolve({saved:true});});
  await page.waitForFunction(()=>pendingDecisions.size===0);
  assert.deepEqual(await page.evaluate(()=>saves.map(s=>[s.body.key,s.body.decision])),[['row0','confirmed'],['row1','denied'],['row2','confirmed']]);
  assert.equal(await page.evaluate(()=>matchReview.candidateCount),98);
  // Clear a complete window rapidly. Refill must wait for durable results,
  // happen exactly once, and never skip a failed/restored row.
  await page.evaluate(()=>{clearTimeout(matchReconcileTimer);matchReview={...matchReview,candidates:[candidate('last1'),candidate('last2')],candidateCount:3,candidateStreamCount:3};renderMatchReview();});
  await row('last1').locator('.confirm-button').click();await row('last2').locator('.deny-button').click();
  await page.waitForFunction(()=>saves.length===4);assert.equal(await page.evaluate(()=>loads),0);
  await page.evaluate(()=>saves[3].resolve({saved:true}));await page.waitForFunction(()=>saves.length===5);assert.equal(await page.evaluate(()=>loads),0);
  await page.evaluate(()=>saves[4].resolve({saved:true}));await page.waitForFunction(()=>loads===1);
  // A new source/reset epoch must fence both the in-flight reply and choices
  // still waiting to be sent. No request may be replayed into the new source.
  await page.evaluate(()=>{clearTimeout(matchReconcileTimer);matchReview={...matchReview,candidates:[candidate('old1'),candidate('old2')],candidateCount:2};renderMatchReview();});
  await row('old1').locator('.confirm-button').click();await row('old2').locator('.deny-button').click();await page.waitForFunction(()=>saves.length===6);
  await page.evaluate(()=>{dispatchReviewEpoch++;matchReview={...matchReview,candidates:[candidate('new-source')],candidateCount:1};renderMatchReview();saves[5].resolve({saved:true});});
  await page.waitForFunction(()=>pendingDecisions.size===0);assert.equal(await page.evaluate(()=>saves.length),6);assert.equal(await row('new-source').count(),1);
  // Two successful low-score primaries cannot replace each other's popup.
  await page.evaluate(()=>{clearTimeout(matchReconcileTimer);const a=candidate('primary-a',82),b=candidate('primary-b',94);a.alternatives=[candidate('alt-a',94)];b.alternatives=[candidate('alt-b',95)];draft.channels=[{id:'alt-a',included:true},{id:'alt-b',included:true}];matchReview={...matchReview,candidates:[a,b],candidateCount:4};renderMatchReview();});
  await row('primary-a').locator('.confirm-button').click();await row('primary-b').locator('.deny-button').click();await page.waitForFunction(()=>saves.length===7);
  await page.evaluate(()=>saves[6].resolve({saved:true}));await page.waitForFunction(()=>saves.length===8);await page.evaluate(()=>saves[7].resolve({saved:true}));
  await page.waitForFunction(()=>pendingDecisions.size===0);assert.equal(await page.locator('#match-alternative-title').textContent(),'Other matches for primary-a');
  assert.equal(await page.evaluate(()=>pendingAlternativeReviews.length),1);
  const popup=page.locator('#match-alternative-dialog');await popup.getByRole('button',{name:'Deny',exact:true}).click();await page.waitForFunction(()=>saves.length===9);
  assert.equal(await popup.evaluate(n=>n.open),true);await page.evaluate(()=>saves[8].reject(Error('retryable popup save')));await page.waitForFunction(()=>pendingDecisions.size===0);
  assert.equal(await popup.evaluate(n=>n.open),true);await popup.getByRole('button',{name:'Confirm this match',exact:true}).click();await page.waitForFunction(()=>saves.length===10);await page.evaluate(()=>saves[9].resolve({saved:true}));
  await page.waitForFunction(()=>document.getElementById('match-alternative-title').textContent==='Other matches for primary-b');assert.equal(await popup.evaluate(n=>n.open),true);
  await popup.getByRole('button',{name:'Deny',exact:true}).click();await page.waitForFunction(()=>saves.length===11);await page.evaluate(()=>saves[10].resolve({saved:true}));await page.waitForFunction(()=>!document.getElementById('match-alternative-dialog').open);assert.equal(await page.evaluate(()=>matchReview.candidateCount),0);
  assert.equal(await page.evaluate(()=>saves.every(s=>Array.isArray(s.body.tvgIds)&&s.body.tvgIds.length===0)),true,'primary and popup writes must omit provider IDs');
  console.log(`Responsive review: ${dimensions.streams}-stream view, ${dimensions.rows} rows/${dimensions.decisions} decisions/${dimensions.constraints} constraints; next choice accepted in ${elapsed.toFixed(1)}ms while prior save pending; failure isolation and background draft overlap pass`);
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
