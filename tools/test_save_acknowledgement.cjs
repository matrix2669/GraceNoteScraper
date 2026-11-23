const {chromium}=require('playwright');
const vm=require('node:vm');
const fs=require('node:fs'),assert=require('node:assert/strict');
async function transportCases(){
 const html=fs.readFileSync('lineuparr.html','utf8');const apiCode=html.slice(html.indexOf('    async function api('),html.indexOf('    let builderResetBackup'));
 const timers=new Set();let aborted=0;
 const c={draft:{resetGeneration:2},Headers,AbortController,fetch:null,setTimeout(fn,ms){const id=setTimeout(()=>{timers.delete(id);fn()},Math.min(ms,20));timers.add(id);return id},clearTimeout(id){timers.delete(id);clearTimeout(id)}};vm.createContext(c);vm.runInContext(apiCode,c);
 const response=(json,ok=true)=>({ok,status:ok?200:409,headers:new Headers({'content-type':'application/json'}),json,text:json});
 const never=()=>new Promise(()=>{});
 for(const name of ['hung-fetch','hung-success-body','hung-error-body','abort-ignored']){
  c.fetch=async(_,options)=>{options.signal.addEventListener('abort',()=>aborted++);if(name==='hung-fetch'||name==='abort-ignored')return never();return response(never,name!=='hung-error-body')};
  await assert.rejects(c.api('/api/lineuparr/dispatcharr/decision',{method:'POST',reviewDeadline:15000}),e=>e.uncertain===true);assert.equal(timers.size,0,name+' timer cleanup');
 }
 c.fetch=async()=>response(async()=>{throw Error('truncated body')});await assert.rejects(c.api('/api/lineuparr/dispatcharr/decision',{method:'POST',reviewDeadline:15000}),e=>e.uncertain===true);assert.equal(timers.size,0);
 c.fetch=async()=>response(async()=>{throw Error('truncated error body')},false);await assert.rejects(c.api('/api/lineuparr/dispatcharr/decision',{method:'POST',reviewDeadline:15000}),e=>e.uncertain===true);assert.equal(timers.size,0);
 let release;c.fetch=()=>new Promise(resolve=>release=resolve);await assert.rejects(c.api('/api/lineuparr/dispatcharr/decision',{method:'POST',reviewDeadline:15000}),e=>e.uncertain===true);release(response(async()=>({saved:true})));await Promise.resolve();assert.equal(timers.size,0,'ignored abort and late body do not leave timers');
 c.fetch=async()=>response(async()=>({saved:true}));assert.equal((await c.api('/api/lineuparr/dispatcharr/decision',{method:'POST',reviewDeadline:15000})).saved,true);assert.equal(timers.size,0);
 let longRelease;c.fetch=()=>new Promise(resolve=>longRelease=resolve);const long=c.api('/api/lineuparr/dispatcharr/review?refresh=true');assert.equal(timers.size,0,'unrelated API has no deadline');longRelease(response(async()=>({current:true})));assert.equal((await long).current,true);assert.ok(aborted>=4);
 console.log('Actual API: fetch, success/error body, truncated body, ignored abort, late completion, timer cleanup and unrelated long request pass');
}

(async()=>{
 await transportCases();
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
 try {
  const page=await browser.newPage();await page.route('**/*',r=>r.abort());
  const html=fs.readFileSync('lineuparr.html','utf8');
  await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
  const apiCode=html.slice(html.indexOf('    async function api('),html.indexOf('    let builderResetBackup'));
  const impactCode=html.slice(html.indexOf('    let supplementalImpactRequest ='),html.indexOf('    function renderAutomaticExclusions('));
  const code=apiCode+impactCode+html.slice(html.indexOf('    function renderAutomaticExclusions('),html.indexOf('    async function clearDecision('))+html.slice(html.indexOf('    function setDispatchControls('),html.indexOf('    function showConfirmedMatches('))+html.slice(html.indexOf("    els.alternativeDialog.addEventListener('close'"),html.indexOf("    document.getElementById('builder-reset').addEventListener"));
  await page.evaluate(code=>{
   window.els={};for(const [key,id] of Object.entries({matchReview:'match-review',matchStreams:'match-streams',matchCandidates:'match-candidates',matchConfirmed:'match-confirmed',matchConfirmedJump:'match-confirmed-jump',matchDenied:'match-denied',matchList:'match-list',dispatchLoading:'dispatch-loading',matchStatus:'match-status',decisionList:'decision-list',decisionHistory:'decision-history',alternativeDialog:'match-alternative-dialog',alternativeList:'match-alternative-list',alternativeTitle:'match-alternative-title',alternativeHelp:'match-alternative-help'}))els[key]=document.getElementById(id);
   for(const [key,id] of Object.entries({exportOpen:'export-open',dispatchURL:'dispatch-url',dispatchAuth:'dispatch-auth',dispatchUser:'dispatch-user',dispatchPassword:'dispatch-password',dispatchAPIKey:'dispatch-api-key',dispatchSave:'dispatch-save',dispatchRefresh:'dispatch-refresh',dispatchDisconnect:'dispatch-disconnect'}))els[key]=document.getElementById(id);
   Object.assign(window,{draft:{sourceFingerprint:'fixture',channels:[]},publishing:false,saving:false,pendingChannelSaves:0,pendingDecisions:new Set(),dispatchBusy:false,decisionRevision:0,dispatchReviewEpoch:0,workflowRemote:{},matchReviewLimit:100,decisionSaveQueue:Promise.resolve(),matchReconcileTimer:null,pendingAlternativeReviews:[],builderResetPending:false});
   for(const name of ['renderGuidedWorkflow','scheduleWorkflowRefresh','renderSummary'])window[name]=()=>{};
   window.messages=[];window.showMessage=(text,error)=>messages.push({text,error});
   window.emptyMessage=text=>{const p=document.createElement('p');p.textContent=text;return p};
   window.saves=[];window.drafts=[];

   window.reloadDraft=async(epoch,revision)=>new Promise(resolve=>drafts.push({epoch,revision,resolve}));
   window.candidate=(key,score=100)=>({key,channelId:key,reviewKind:score<95?'inclusion':'qualified',alias:key,streamNames:[key],streamCount:1,minimumScore:score,maximumScore:score,channelNumber:'1',channelName:'Fixture',reasons:[],alternatives:[]});
   window.matchReview={streamCount:21208,candidateCount:100,confirmedCount:722,deniedCount:0,candidates:Array.from({length:100},(_,i)=>candidate('row'+i)),decisions:Array.from({length:722},(_,i)=>({key:'old'+i,decision:'confirmed',streamName:'Saved '+i,channelName:'Fixture',streamCount:1,score:100})),automatic:{rows:Array.from({length:800},(_,i)=>({key:'auto'+i,exported:true,streamName:'Synthetic '+i,channelNumber:'1',channelName:'Fixture',reason:'identity fixture'}))}};
   window.loads=0;window.loadMatchReview=async()=>{loads++;recoverMatchActions.refreshAdmission=false;if(!matchReview.candidates.length&&matchReview.candidateCount>0)matchReview.candidates=[candidate('refilled')];renderMatchReview();};
   (0,eval)(code+'\nwindow.cancelValidation=()=>clearTimeout(validationPollTimer);window.renderMatchReview=renderMatchReview;window.decideMatch=decideMatch;window.decideAlternative=decideAlternative;window.openMatchAlternatives=openMatchAlternatives;');renderMatchReview();
  },code);
  await page.evaluate(()=>{
   const originalSetTimeout=window.setTimeout.bind(window);
   window.setTimeout=(fn,ms,...args)=>originalSetTimeout(fn,ms===15000?60:ms===5000?40:ms,...args);
   window.behavior='hang'; window.receiptState='busy'; window.receipts=0; window.postBodies=[]; window.late=[];
   window.reply=(data,status=200)=>({ok:status<400,status,headers:new Headers({'content-type':'application/json'}),json:async()=>data,text:async()=>JSON.stringify(data)});
   window.fetch=async(path,options={})=>{
    if(options.method==='POST') {
     const body=JSON.parse(options.body);postBodies.push(body);
     if(behavior==='reject')return reply({error:'definite rejection'},409);
     if(behavior==='malformed')return reply({saved:false});
     if(behavior==='success')return reply({saved:true});
     return new Promise(resolve=>late.push(()=>resolve(reply({saved:true}))));
    }
    if(path.includes('decision?operation=')){
     receipts++;
     if(receiptState==='hang')return new Promise(()=>{});
     if(receiptState==='offline')throw Error('receipt offline');
     if(receiptState==='busy')return reply({error:'busy'},409);
     const op=decodeURIComponent(path.split('operation=')[1]);
     const body=postBodies.find(b=>b.operation===op);
     return reply({operation:receiptState==='bad-operation'?'wrong':op,key:receiptState==='bad-key'?'wrong':body?.key,decision:receiptState==='bad-decision'?'denied':body?.decision,state:receiptState.startsWith('bad-')?'saved':receiptState});
    }
    if(path.includes('/review?'))return reply({...matchReview,decisionTokens:Object.fromEntries(Object.keys(matchReview.decisionTokens).map(k=>[k,'fresh-'+k]))});
    return reply({state:'idle'});
   };
   window.loadMatchReview=async()=>{loads++;recoverMatchActions.refreshAdmission=false;if(!matchReview.candidates.length&&matchReview.candidateCount>0)matchReview.candidates=[candidate('refilled')];renderMatchReview();};
   window.reloadDraft=async()=>true;
   window.resetFixture=(keys)=>{
    clearTimeout(matchReconcileTimer);cancelValidation();
    matchActionState.states=new Map();pendingDecisions.clear();decisionSaveQueue=Promise.resolve();
    postBodies=[];late=[];receipts=0;loads=0;behavior='hang';receiptState='busy';recoverMatchActions.refreshAdmission=false;
    matchReview={...matchReview,reviewStatus:'current',operationScope:'scope',decisionTokens:Object.fromEntries(keys.map(k=>[k,'op-'+k])),candidates:keys.map(k=>candidate(k)),decisions:[],candidateCount:keys.length,candidateStreamCount:keys.length,confirmedCount:0,deniedCount:0,supplementalImpact:{state:'idle'}};
    draft.channels=keys.map(id=>({id,included:true}));renderMatchReview();
   };
   resetFixture(['first','second','third']);
  });

  const row=key=>page.locator(`#match-list > .match-card[data-key="${key}"]`);
  await row('first').locator('.confirm-button').click();
  await row('second').locator('.deny-button').click();
  await row('third').locator('.confirm-button').click();
  await page.waitForFunction(()=>matchActionState('first').state==='uncertain'&&receipts===1);
  assert.equal(await page.evaluate(()=>postBodies.length),1,'unverified predecessor fences all later POSTs');
  assert.equal(await page.evaluate(()=>pendingDecisions.size),3);
  assert.equal(await page.locator('#match-confirmed').innerText(),'0','unsettled intentions cannot display as completed confirmations');
  assert.equal(await page.locator('#match-denied').innerText(),'0');
  assert.equal(await page.locator('#match-candidates').innerText(),'3');
  assert.equal(await row('first').count(),1,'uncertainty keeps a recoverable main card');
  assert.equal(await row('first').locator('.deny-button').isDisabled(),true);
  assert.equal(await page.locator('#match-save-recovery button').isEnabled(),true,'recovery works despite logical pending jobs');
  assert.equal(await page.locator('#export-open').isDisabled(),true);
  assert.equal(await page.locator('#match-reload').isVisible(),false);
  assert.equal(await page.getByText('Validation details',{exact:true}).count(),0);
  if(process.env.SAVE_ACK_SCREENSHOT_DIR)await page.locator('#match-review').screenshot({path:process.env.SAVE_ACK_SCREENSHOT_DIR+'/ordered-uncertain.png'});
  // All invalid and unavailable receipts preserve the original queue barrier.
  for(const state of ['bad-operation','bad-key','bad-decision','hang','offline','busy']){
   await page.evaluate(state=>{receiptState=state;},state);
   await page.evaluate(()=>recoverMatchActions());
   assert.equal(await page.evaluate(()=>matchActionState('first').state),'uncertain',state);
   assert.equal(await page.evaluate(()=>postBodies.length),1,state+' cannot retry POST');
  }
  await page.evaluate(()=>{receiptState='saved';behavior='success';});
  await page.locator('#match-save-recovery button').click();
  await page.waitForFunction(()=>pendingDecisions.size===0);
  assert.deepEqual(await page.evaluate(()=>postBodies.map(b=>[b.key,b.decision,b.operation])),[['first','confirmed','op-first'],['second','denied','op-second'],['third','confirmed','op-third']]);
  assert.equal(await page.evaluate(()=>matchReview.confirmedCount),2);assert.equal(await page.evaluate(()=>matchReview.deniedCount),1);
  assert.equal(await row('first').count(),0);
  await page.evaluate(()=>late.forEach(f=>f()));
  assert.equal(await page.evaluate(()=>matchReview.confirmedCount),2,'late ACK cannot count twice');
  // Receipt not-saved retires the original choice, restores once, and lets the
  // independently accepted later choice drain without a blind predecessor retry.
  await page.evaluate(()=>resetFixture(['unsaved','later']));
  await row('unsaved').locator('.confirm-button').click();await row('later').locator('.deny-button').click();
  await page.waitForFunction(()=>receipts===1);await page.evaluate(()=>{receiptState='not-saved';behavior='success';return recoverMatchActions();});
  await page.waitForFunction(()=>pendingDecisions.size===0);
  assert.deepEqual(await page.evaluate(()=>postBodies.map(b=>b.key)),['unsaved','later']);
  assert.equal(await row('unsaved').count(),1);assert.equal(await page.evaluate(()=>matchReview.confirmedCount),0);assert.equal(await page.evaluate(()=>matchReview.deniedCount),1);
  // An ordinary definite HTTP rejection also restores only its own row.
  await page.evaluate(()=>{resetFixture(['reject']);behavior='reject';});await row('reject').locator('.deny-button').click();await page.waitForFunction(()=>pendingDecisions.size===0);
  assert.equal(await page.evaluate(()=>matchActionState('reject').state),'failed');assert.equal(await page.evaluate(()=>receipts),0);
  // Malformed successful ACK must be reconciled rather than invented success.
  await page.evaluate(()=>{resetFixture(['malformed']);behavior='malformed';});await row('malformed').locator('.confirm-button').click();await page.waitForFunction(()=>receipts===1);
  assert.equal(await page.evaluate(()=>matchActionState('malformed').state),'uncertain');
  await page.evaluate(()=>{receiptState='saved';return recoverMatchActions();});await page.waitForFunction(()=>pendingDecisions.size===0);
  // Old scope intentions can never cross restart/supersession, source or epoch replacement.
  for(const reason of ['superseded','undone','source','epoch','scope']){
   await page.evaluate(()=>resetFixture(['old','queued']));await row('old').locator('.confirm-button').click();await row('queued').locator('.deny-button').click();await page.waitForFunction(()=>receipts===1);
   await page.evaluate(reason=>{if(reason==='source')draft={...draft,sourceFingerprint:'new-source'};else if(reason==='epoch')dispatchReviewEpoch++;else if(reason==='scope')matchReview.operationScope='replacement';else receiptState=reason;if(['source','epoch','scope'].includes(reason))receiptState='superseded';return recoverMatchActions();},reason);
   await page.waitForFunction(()=>pendingDecisions.size===0);
   assert.equal(await page.evaluate(()=>postBodies.length),1,reason+' must cancel old queued transport');
   assert.equal(await page.evaluate(()=>matchActionState('old').state),'cancelled');
   await page.evaluate(()=>{draft={...draft,sourceFingerprint:'fixture'};late.forEach(f=>f());});
   assert.equal(await page.evaluate(()=>matchActionState('old').state),'cancelled');
  }
  // Final popup: deadline frees Saving, recovery is reachable inside the modal,
  // and receipt settlement runs the ordinary close/refill path exactly once.
  await page.evaluate(()=>{
   resetFixture(['popup']);matchReview.candidates=[];matchReview.candidateCount=2;
   window.primary={...candidate('primary',80),alternatives:[candidate('popup')]};
   openMatchAlternatives(primary);
  });
  const popup=page.locator('#match-alternative-dialog');await popup.getByRole('button',{name:'Confirm this match',exact:true}).click();
  await page.waitForFunction(()=>receipts===1);assert.match(await popup.innerText(),/Save not yet verified/);
  assert.equal(await popup.getByRole('button',{name:'Check saved state',exact:true}).isEnabled(),true);assert.equal(await popup.getByRole('button',{name:'Deny',exact:true}).isDisabled(),true);
  assert.equal(await popup.getByRole('button',{name:'Close alternate matches'}).isEnabled(),true);
  if(process.env.SAVE_ACK_SCREENSHOT_DIR)await popup.screenshot({path:process.env.SAVE_ACK_SCREENSHOT_DIR+'/popup-uncertain.png'});
  await page.evaluate(()=>{receiptState='saved';behavior='success';});await popup.getByRole('button',{name:'Check saved state',exact:true}).click();
  await page.waitForFunction(()=>!document.getElementById('match-alternative-dialog').open&&pendingDecisions.size===0&&loads===1);
  assert.equal(await page.evaluate(()=>matchReview.confirmedCount),1);await page.evaluate(()=>late.forEach(f=>f()));assert.equal(await page.evaluate(()=>matchReview.confirmedCount),1);
  // A definitive not-saved popup remains retryable with fresh admission while
  // preserving the modal and its original raw choice.
  await page.evaluate(()=>{resetFixture(['popup-unsaved']);matchReview.candidates=[];window.primary={...candidate('p',80),alternatives:[candidate('popup-unsaved')]};openMatchAlternatives(primary);});
  await popup.getByRole('button',{name:'Confirm this match',exact:true}).click();await page.waitForFunction(()=>receipts===1);
  await page.evaluate(()=>{receiptState='not-saved';return recoverMatchActions();});await page.waitForFunction(()=>pendingDecisions.size===0);
  assert.equal(await popup.evaluate(n=>n.open),true);assert.match(await popup.innerText(),/Choice not saved/);
  await page.waitForFunction(()=>!matchActionState('popup-unsaved').needsAdmission);
  await page.evaluate(()=>{behavior='success';});await popup.getByRole('button',{name:'Confirm this match',exact:true}).click();await page.waitForFunction(()=>pendingDecisions.size===0&&!document.getElementById('match-alternative-dialog').open);
  assert.deepEqual(await page.evaluate(()=>postBodies.map(b=>b.operation)),['op-popup-unsaved','fresh-popup-unsaved'],'explicit retry uses fresh admission only');
  // Receipt reconciliation cannot invalidate the accepted alternative backlog.
  await page.evaluate(()=>{resetFixture(['p1','p2','survivor']);for(const key of ['p1','p2']){const c=matchReview.candidates.find(c=>c.key===key);c.minimumScore=80;c.alternatives=[candidate('alt-'+key)];draft.channels.push({id:'alt-'+key,included:true});matchReview.decisionTokens['alt-'+key]='op-alt-'+key;}matchReview.candidateCount=5;renderMatchReview();});
  await row('p1').locator('.confirm-button').click();await row('p2').locator('.deny-button').click();await page.waitForFunction(()=>receipts===1);
  await page.evaluate(()=>{receiptState='saved';behavior='success';return recoverMatchActions();});await page.waitForFunction(()=>pendingDecisions.size===0&&document.getElementById('match-alternative-dialog').open);
  await page.evaluate(()=>new Promise(r=>setTimeout(r,450)));assert.equal(await page.evaluate(()=>loads),0,'deferred read cannot replace popup epochs');
  assert.equal(await page.locator('#match-alternative-title').innerText(),'Other matches for p1');
  await popup.getByRole('button',{name:'Confirm this match',exact:true}).click();await page.waitForFunction(()=>document.getElementById('match-alternative-title').textContent==='Other matches for p2');
  await popup.getByRole('button',{name:'Deny',exact:true}).click();await page.waitForFunction(()=>pendingDecisions.size===0&&!document.getElementById('match-alternative-dialog').open);
  await page.waitForFunction(()=>loads===1);assert.equal(await row('survivor').count(),1);
  // Closing during uncertainty does not reopen an obsolete modal on recovery.
  await page.evaluate(()=>{resetFixture(['closed']);matchReview.candidates=[];window.primary={...candidate('p',80),alternatives:[candidate('closed')]};openMatchAlternatives(primary);});
  await popup.getByRole('button',{name:'Deny',exact:true}).click();await page.waitForFunction(()=>receipts===1);await popup.getByRole('button',{name:'Close alternate matches'}).click();
  await page.evaluate(()=>{receiptState='saved';return recoverMatchActions();});await page.waitForFunction(()=>pendingDecisions.size===0);assert.equal(await popup.evaluate(n=>n.open),false);
  // Presentation: raw strings and independent alias arrays never invent pairings/feed.
  await page.evaluate(()=>{resetFixture(['names']);const c={...candidate('names'),channelNumber:'905',channelDisplayName:'HBO Drama',channelCallSign:'HBODHDP',streamNames:['TV:HBODRAMA'],exactAliases:['HBO Drama West'],targetFeed:{region:'west'},streamFeeds:[{region:'unknown'}]};matchReview.candidates=[c];renderMatchReview();});
  assert.match(await row('names').innerText(),/Matched alias: “TV:HBODRAMA” → “HBO Drama West”/);assert.match(await row('names').innerText(),/905 · HBO Drama West \(HBODHDP\)/);assert.doesNotMatch(await row('names').innerText(),/Stream feed:/);assert.equal(await row('names').locator('.deny-button').isVisible(),true);
  await page.evaluate(()=>{matchReview.candidates[0].streamNames=['Stream TV: HBO DRAMA'];matchReview.candidates[0].alias='Stream TV: HBO DRAMA';renderMatchReview();});assert.match(await row('names').locator('.match-reason').innerText(),/Matched alias: “Stream TV: HBO DRAMA” → “HBO Drama West”/);assert.doesNotMatch(await row('names').innerText(),/Stream feed: West/);if(process.env.SAVE_ACK_SCREENSHOT_DIR)await row('names').screenshot({path:process.env.SAVE_ACK_SCREENSHOT_DIR+'/raw-stream-west-alias.png'});
  await page.evaluate(()=>{matchReview.candidates[0].streamNames=['HBO Drama'];renderMatchReview();});assert.match(await row('names').innerText(),/Matched alias: “HBO Drama” → “HBO Drama West”/);
  await page.evaluate(()=>{matchReview.candidates[0].streamNames=['A','B'];matchReview.candidates[0].exactAliases=['Alias A','Alias B'];renderMatchReview();});assert.match(await row('names').innerText(),/Matched aliases: “Alias A”; “Alias B”/);assert.doesNotMatch(await row('names').locator('.match-reason').innerText(),/→/);
  await page.evaluate(()=>renderSupplementalImpact({state:'complete',unresolved:true,rows:[{channelNumber:'905',channelName:'HBO Drama West',missed:['TV:HBODRAMA'],protectedConflicts:[{name:'Saved raw'}]}]}));assert.match(await page.locator('#supplemental-impact-results').innerText(),/TV:HBODRAMA.*Saved raw.*Undo/);assert.equal(await page.locator('#match-reload').isVisible(),false);
  await page.evaluate(()=>renderSupplementalImpact({state:'failed'}));assert.equal(await page.locator('#match-reload').isVisible(),true);
  // R1-F1: zero proposals cannot hide the pending reason for incomplete Step 6.
  for(const state of ['queued','preparing','running']) {
   const active=await page.evaluate(({html,state})=>{
    resetFixture([]);window.workflowProgress={};window.dispatchConnectionError=false;
    matchReview.supplementalImpact={state,rows:[],unresolved:false};
    workflowRemote={dispatchReady:true,dispatchConfig:{configured:true},dispatch:matchReview};
    const start=html.indexOf('    function workflowStates('),end=html.indexOf('    function renderGuidedWorkflow(',start);
    (0,eval)(html.slice(start,end)+';window.reviewStates=workflowStates;');renderMatchReview();
    document.getElementById('dispatch-panel').open=true;
    return {step6:reviewStates().find(s=>s.step===6).complete,status:document.getElementById('supplemental-impact-status').textContent,findings:document.getElementById('supplemental-impact-results').textContent,reloadHidden:document.getElementById('match-reload').hidden,remaining:document.getElementById('match-candidates').textContent};
   },{html,state});
   assert.equal(active.step6,false,state+' must keep Step6 incomplete');
   assert.match(active.status,/Background validation is pending.*not yet complete/,state+' must explain incompleteness');
   assert.equal(active.remaining,'0');assert.equal(active.findings,'');assert.equal(active.reloadHidden,true);
   assert.equal(await page.locator('#supplemental-impact-status').isVisible(),true);
   assert.equal(await page.getByText('Validation details',{exact:true}).count(),0);
   if(process.env.REVIEW_OUTPUT)await page.locator('#match-review').screenshot({path:process.env.REVIEW_OUTPUT+'/'+state+'-validation.png'});
  }
  // R1-F2: a failed authoritative read cannot invent a saved/failed write result.
  for(const outcome of ['not-saved','saved']) {
   await page.evaluate(outcome=>{
    resetFixture(['read-failure-'+outcome]);messages=[];
    window.reloadDraft=async()=>{throw Error('draft offline')};
    behavior=outcome==='saved'?'success':'hang';
    window.readFailureChoice=decideMatch('read-failure-'+outcome,'confirmed');
   },outcome);
   if(outcome==='not-saved') {
    await page.waitForFunction(()=>receipts===1);
    await page.evaluate(async()=>{receiptState='not-saved';await recoverMatchActions();});
   }
   await page.evaluate(()=>readFailureChoice);
   await page.waitForFunction(()=>messages.some(m=>m.text==='Could not update the draft after review: draft offline'));
   const result=await page.evaluate(outcome=>({outcome,action:matchActionState('read-failure-'+outcome).state,confirmedCount:matchReview.confirmedCount,postCount:postBodies.length,messages}),outcome);
   assert.equal(result.action,outcome==='saved'?'saved':'failed');
   assert.equal(result.confirmedCount,outcome==='saved'?1:0);assert.equal(result.postCount,1,'read recovery must never repeat POST');
   assert.equal(result.messages.at(-1).error,true);assert.equal(result.messages.at(-1).text,'Could not update the draft after review: draft offline');
   if(outcome==='not-saved')assert.ok(result.messages.every(m=>!/choices saved/i.test(m.text)),'not-saved must never be announced saved');
   assert.doesNotMatch(result.messages.at(-1).text,/choices saved|save failed|write failed/i);
   console.log('R1-F2 '+JSON.stringify(result));
  }
  await page.evaluate(()=>{window.reloadDraft=async()=>true;cancelValidation();clearTimeout(matchReconcileTimer);});
  console.log('R1-F1/F2: queued/preparing/running zero-proposal incomplete reason and verified saved/not-saved outcomes survive draft-read failures');
  await page.evaluate(()=>{cancelValidation();clearTimeout(matchReconcileTimer);});
  console.log('Bounded acknowledgement: ordered retained barrier, one automatic receipt, mismatch/offline/409/timeouts, saved/not-saved, source/epoch/Undo fences, modal recovery/refill and truthful presentation pass');
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
