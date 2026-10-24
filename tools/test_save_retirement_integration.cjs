const fs=require('node:fs'),assert=require('node:assert/strict'),{spawnSync}=require('node:child_process');
if(!process.env.SAVE_RETIREMENT_URL){
 const r=spawnSync('go',['test','-race','.','-run','^TestSaveRetirementBrowserIntegration$','-count=1','-timeout=240s','-v'],{encoding:'utf8',env:{...process.env,REVIEW_BROWSER_NODE:process.execPath},timeout:250000});process.stdout.write(r.stdout||'');process.stderr.write(r.stderr||'');process.exitCode=r.status||0;
}else{
 const {chromium}=require('playwright');
 (async()=>{
  const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
  const html=fs.readFileSync('lineuparr.html','utf8'),part=(a,b)=>html.slice(html.indexOf(a),html.indexOf(b,html.indexOf(a)));
  const code=part('    async function api(','    let builderResetBackup')+part('    async function loadMatchReview(','    function showConfirmedMatches(')+part('    let draftRequest = 0;','    function renderAll(')+part('    let matchInputSignature = null;','    function renderSummary(')+part("    els.alternativeDialog.addEventListener('close'","    document.getElementById('builder-reset').addEventListener");
  console.log('Browser version:',await browser.version());
  const origin=process.env.SAVE_RETIREMENT_URL;
  let page,errors=[];
  async function fixture(kind){
   if(page)await page.close();errors=[];page=await browser.newPage();page.on('pageerror',e=>errors.push(e.message));
   await page.route('**/*',r=>r.request().url().startsWith(origin+'/')?r.continue():r.abort());await page.goto(origin+'/empty');
   const fixture=await page.evaluate(async kind=>{const r=await fetch('/fixture?kind='+kind);if(!r.ok)throw Error(await r.text());return r.json()},kind);
   await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
   await page.evaluate(({code,fixture,kind})=>{
    window.els={};for(const [key,id]of Object.entries({matchReview:'match-review',matchStreams:'match-streams',matchCandidates:'match-candidates',matchConfirmed:'match-confirmed',matchConfirmedJump:'match-confirmed-jump',matchDenied:'match-denied',matchList:'match-list',dispatchLoading:'dispatch-loading',matchStatus:'match-status',decisionList:'decision-list',decisionHistory:'decision-history',alternativeDialog:'match-alternative-dialog',alternativeList:'match-alternative-list',alternativeTitle:'match-alternative-title',alternativeHelp:'match-alternative-help',exportOpen:'export-open',dispatchURL:'dispatch-url',dispatchAuth:'dispatch-auth',dispatchUser:'dispatch-user',dispatchPassword:'dispatch-password',dispatchAPIKey:'dispatch-api-key',dispatchSave:'dispatch-save',dispatchRefresh:'dispatch-refresh',dispatchDisconnect:'dispatch-disconnect'}))els[key]=document.getElementById(id);
    Object.assign(window,{draft:fixture.draft,matchReview:fixture.review,dispatchConfig:{configured:true},publishing:false,saving:false,pendingChannelSaves:0,pendingDecisions:new Set(),dispatchBusy:false,decisionRevision:0,dispatchReviewEpoch:0,matchReviewRequest:0,dispatchBusyRequest:0,workflowRemote:{},matchReviewLimit:100,decisionSaveQueue:Promise.resolve(),matchReconcileTimer:null,pendingAlternativeReviews:[],builderResetPending:false,dispatchConnectionError:false});
    // The actual invalidator, renderer, recovery renderer, draft/review readers
    // and controls run. Only unrelated sections/workflow presentation are omitted.
    for(const name of ['renderGuidedWorkflow','scheduleWorkflowRefresh','refreshResetRecovery'])window[name]=()=>{};
    window.renderAll=()=>invalidateChangedMatchInputs();
    window.messages=[];window.showMessage=(text,error)=>messages.push({text,error});window.emptyMessage=text=>{const p=document.createElement('p');p.textContent=text;return p};
    window.mode='normal';window.gateway=502;window.receiptMode='real';window.postBodies=[];window.receiptReads=0;window.responseReady=false;window.failDraft=false;window.fastDeadline=false;window.readPolicy='normal';window.authoritativeReads=[];window.releaseReads=[];
    const realFetch=window.fetch.bind(window),realTimeout=window.setTimeout.bind(window);
    window.setTimeout=(fn,ms,...args)=>realTimeout(fn,fastDeadline&&ms===15000?100:receiptMode==='hang'&&ms===5000?50:ms,...args);
    window.fetch=async(path,options={})=>{
     if(readPolicy!=='normal'&&(String(path).includes('/lineuparr/draft')||String(path).includes('/dispatcharr/review?'))){
      const record={path:String(path),finished:false};authoritativeReads.push(record);
      try {if(readPolicy==='fail')throw Error('isolated authoritative read offline');await new Promise(resolve=>releaseReads.push(resolve));return await realFetch(path,options)}
      finally {record.finished=true}
     }
     if(String(path).includes('/lineuparr/draft')&&failDraft)throw Error('isolated draft offline');
     if(String(path).includes('/dispatcharr/decision?operation=')){
      receiptReads++;
      if(receiptMode==='busy')return new Response('Synthetic intermediary busy',{status:409});
      if(receiptMode==='offline')throw Error('receipt offline');
      if(receiptMode==='hang')return new Promise(()=>{});
      const response=await realFetch(path,options);
      if(receiptMode.startsWith('bad-')){const body=await response.json();body[receiptMode.slice(4)]='wrong';return new Response(JSON.stringify(body),{status:200,headers:{'content-type':'application/json'}})}
      return response;
     }
     if(options.method==='POST'&&String(path).includes('/dispatcharr/decision')){
      const body=JSON.parse(options.body);postBodies.push(body);
      if(body.key!==keys[0])return realFetch(path,options);
      if(mode==='reject')return realFetch(path,{...options,body:JSON.stringify({...body,operation:'not-current'})});
      if(mode==='before'){window.delayedWrite=()=>realFetch(path,options);return new Response('upstream outcome unavailable',{status:gateway})}
      if(mode==='upstream'){
       const headers=new Headers(options.headers);headers.set('X-Test-Hold','true');window.upstream=realFetch(path,{...options,headers});
       while(!(await (await realFetch('/test/inspection')).json()).held)await new Promise(r=>realTimeout(r,2));
       return new Response('upstream still executing',{status:gateway});
      }
      const response=await realFetch(path,options);if(!response.ok)return response;
      window.responseReady=true;
      if(mode==='gateway')return new Response('gateway replaced durable acknowledgement',{status:gateway});
      if(['held','lost','late'].includes(mode)){await new Promise(resolve=>window.releaseAck=resolve);if(mode==='lost')throw Error('lost ACK after durable handler')}
      return response;
     }
     return realFetch(path,options);
    };
    (0,eval)(code+'\nwindow.renderMatchReview=renderMatchReview;window.cancelPoll=()=>clearTimeout(validationPollTimer);');
    matchReview=mergeSupplementalReview(fixture.review);
    const candidates=matchReview.candidates.filter(c=>kind==='supplemental'?!!c.origin:!c.origin);
    if(candidates.length<3)throw Error(kind+' needs at least3 real admitted groups; got'+candidates.length);
    candidates.sort((a,b)=>Number(b.streamCount>1)-Number(a.streamCount>1));
    window.keys=candidates.slice(0,3).map(c=>c.key);window.target=candidates[0].channelId;window.other=draft.channels.find(c=>c.included&&c.id!==target).id;
    // Preserve real candidates/tokens while limiting this rendered test page.
    matchReview={...matchReview,candidates:candidates.slice(0,3),candidateCount:3,candidateStreamCount:candidates.slice(0,3).reduce((n,c)=>n+c.streamCount,0)};
    renderMatchReview();invalidateChangedMatchInputs();
    window.unloadProtected=()=>{const event={preventDefault(){this.prevented=true}};protectUnsavedReview(event);return !!event.prevented};
    window.startBurst=()=>{window.choices=[decideMatch(keys[0],'confirmed'),decideMatch(keys[1],'denied'),decideMatch(keys[2],'confirmed')]};
    window.cancelTimers=()=>{cancelPoll();clearTimeout(matchReconcileTimer)};
   },{code,fixture,kind});
  }
  const state=()=>page.evaluate(()=>({states:keys.map(k=>matchActionState(k).state),posts:postBodies.map(b=>[b.key,b.decision,b.operation]),reads:receiptReads,pending:pendingDecisions.size,confirmed:matchReview.confirmedCount,denied:matchReview.deniedCount,history:matchReview.decisions.map(d=>d.key),candidates:matchReview.candidates.map(c=>c.key),protected:unloadProtected(),messages}));
  async function checkClean(){assert.deepEqual(errors,[]);await page.evaluate(()=>cancelTimers())}
  try{
   for(const kind of ['primary','supplemental'])for(const gateway of [502,503,504]){
    await fixture(kind);
    await page.evaluate(gateway=>{window.gateway=gateway;mode='gateway';receiptMode='busy';startBurst()},gateway);
    await page.waitForFunction(()=>receiptReads===1&&matchActionState(keys[0]).state==='uncertain');
    let result=await state();assert.equal(result.posts.length,1);assert.equal(result.pending,3);assert.equal(result.protected,true);
    if(gateway===502&&process.env.SAVE_RETIREMENT_SCREENSHOT_DIR)await page.locator('#match-review').screenshot({path:process.env.SAVE_RETIREMENT_SCREENSHOT_DIR+'/'+kind+'-gateway-barrier.png'});
    const disk=await page.evaluate(async()=> (await fetch('/test/inspection')).json());assert.ok(disk[kind]>0,'gateway follows real durable '+kind+' write');
    assert.equal(await page.locator('#match-confirmed').innerText(),'0');
    if(gateway===502){
     for(const policy of ['offline','bad-operation','bad-key','bad-decision','hang','busy']){
      await page.evaluate(async policy=>{receiptMode=policy;await recoverMatchActions()},policy);result=await state();assert.equal(result.posts.length,1);assert.equal(result.states[0],'uncertain');
     }
     // A duplicate queued intention cannot become another POST.
     await page.evaluate(()=>decideMatch(keys[1],'denied'));assert.equal((await state()).posts.length,1);
    }
    const accepted=await page.evaluate(()=>keys.map((key,i)=>[key,i===1?'denied':'confirmed',matchReview.decisionTokens[key]]));
    await page.evaluate(async()=>{receiptMode='real';mode='normal';await recoverMatchActions();await Promise.all(choices)});
    result=await state();assert.deepEqual(result.posts,accepted);assert.deepEqual(result.states,['saved','saved','saved']);assert.equal(result.protected,false);assert.equal(result.confirmed,2);assert.equal(result.denied,1);
    await checkClean();console.log(kind+' '+gateway+' durable gateway ACK: barrier/receipt/single ordered drain pass');
    // A gateway can also arrive while the real admitted upstream still holds
    // the mutation lock. Its automatic receipt is an actual handler409.
    await fixture(kind);await page.evaluate(gateway=>{window.gateway=gateway;mode='upstream';startBurst()},gateway);
    await page.waitForFunction(()=>receiptReads===1&&matchActionState(keys[0]).state==='uncertain');assert.equal((await state()).posts.length,1);
    await page.evaluate(async()=>{await fetch('/test/release',{method:'POST'});const r=await upstream;if(!r.ok)throw Error(await r.text());mode='normal';await recoverMatchActions();await Promise.all(choices)});
    assert.deepEqual((await state()).states,['saved','saved','saved']);await checkClean();console.log(kind+' '+gateway+' real unresolved upstream/receipt409 then saved pass');
    // Authoritative absence retires the original operation before delayed
    // arrival; retry is a separate explicit choice with fresh admission.
    await fixture(kind);await page.evaluate(gateway=>{window.gateway=gateway;mode='before';receiptMode='busy';startBurst()},gateway);
    await page.waitForFunction(()=>receiptReads===1);const old=await page.evaluate(()=>postBodies[0].operation);
    await page.evaluate(async()=>{receiptMode='real';mode='normal';await recoverMatchActions();await Promise.all(choices)});
    result=await state();assert.deepEqual(result.states,['failed','saved','saved']);assert.equal(result.posts.length,3);assert.equal(result.confirmed,1);assert.equal(result.denied,1);
    assert.equal(await page.evaluate(async()=> (await delayedWrite()).status),409,'late old operation must be fenced');
    await page.waitForFunction(old=>!matchActionState(keys[0]).needsAdmission&&matchReview.decisionTokens[keys[0]]!==old,old);
    await page.evaluate(()=>decideMatch(keys[0],'confirmed'));result=await state();assert.equal(result.posts.length,4);assert.notEqual(result.posts[3][2],old);assert.equal(result.states[0],'saved');await checkClean();
   }
   for(const kind of ['primary','supplemental']){
    await fixture(kind);await page.evaluate(()=>{mode='reject';startBurst()});await page.evaluate(()=>Promise.all(choices));let result=await state();assert.deepEqual(result.states,['failed','saved','saved']);assert.equal(result.reads,0);await checkClean();
    // Run real input invalidation with the real recovery renderer before ACK.
    for(const scenario of ['held','lost','source','target','lost-source','lost-target','read-failure']){
     await fixture(kind);await page.evaluate(scenario=>{mode=scenario.startsWith('lost')?'lost':'held';receiptMode=scenario==='lost'?'busy':'real';window.a=decideMatch(keys[0],'confirmed');window.b=decideMatch(keys[1],'denied')},scenario);
     await page.waitForFunction(()=>responseReady);
     await page.evaluate(async scenario=>{
      const removed=scenario.includes('target')?target:other;
      await fetch('/test/exclude?id='+encodeURIComponent(removed),{method:'POST'});draft.channels.find(c=>c.id===removed).included=false;
      if(scenario.includes('source')){
       const next=await (await fetch('/test/source',{method:'POST'})).json();draft={...draft,sourceFingerprint:next.source,channels:[]};
       matchReview={...matchReview,operationScope:'new-scope',candidates:[],decisions:[],candidateCount:0,candidateStreamCount:0,confirmedCount:0,deniedCount:0};
      }
      if(scenario==='read-failure')failDraft=true;
      invalidateChangedMatchInputs();
     },scenario);
     result=await state();assert.equal(result.posts.length,1);assert.equal(result.states[0],'saving','renderer cannot cancel transmitted A');assert.equal(result.protected,true);assert.equal(await page.evaluate(()=>matchActionState(keys[0]).job.retired),true);
     await page.evaluate(()=>releaseAck());
     if(scenario==='lost'){
      await page.waitForFunction(()=>receiptReads===1&&matchActionState(keys[0]).state==='uncertain');assert.equal((await state()).protected,true);
      await page.evaluate(async()=>{receiptMode='real';await recoverMatchActions()});
     }
     await page.evaluate(()=>Promise.all([a,b]));result=await state();assert.equal(result.posts.length,1);assert.equal(result.states[1],'cancelled');assert.equal(result.protected,false);assert.equal(result.states[0],scenario==='lost-source'?'cancelled':'saved');
     if(scenario.includes('source')){assert.equal(result.confirmed,0);assert.equal(result.history.length,0);assert.equal(result.candidates.length,0)}
     else {assert.equal(result.confirmed,1);assert.ok(result.history.includes(result.posts[0][0]));assert.ok(!result.candidates.includes(result.posts[0][0]));assert.ok(result.messages.every(m=>!/Cancelled before sending.*transmitted|Save not acknowledged for .*upstream/i.test(m.text)))}
     if(scenario==='lost-source'){const disk=await page.evaluate(async()=> (await fetch('/test/inspection')).json());assert.ok(disk[kind]>0,'superseded receipt must not be presented as proof the original durable choice was unsaved')}
     if(scenario==='held'&&process.env.SAVE_RETIREMENT_SCREENSHOT_DIR){await page.evaluate(()=>els.decisionHistory.open=true);await page.locator('#match-review').screenshot({path:process.env.SAVE_RETIREMENT_SCREENSHOT_DIR+'/'+kind+'-retired-valid-ack.png'})}
     if(scenario.includes('target'))assert.equal(await page.evaluate(()=>matchReview.candidates.some(c=>c.channelId===target)),false);
     if(scenario==='read-failure')await page.waitForFunction(()=>messages.some(m=>m.text.includes('Could not update the draft after review: isolated draft offline')));
     await checkClean();console.log(kind+' actual invalidation/render/recovery '+scenario+' pass');
    }
    // Reverse the original ordering: uncertainty is visible before the other
    // channel is excluded, so production invalidation clones the pending row.
    for(const outcome of ['saved','not-saved'])for(const readPolicy of ['delay','fail'])for(const queued of [false,true]){
     await fixture(kind);
     const setup=await page.evaluate(({outcome,queued})=>{
      const rows=matchReview.candidates.slice(0,queued?2:1);matchReview={...matchReview,candidates:rows,candidateCount:rows.length,candidateStreamCount:rows.reduce((n,c)=>n+c.streamCount,0)};
      window.initialProposal=rows[0];window.reversePhysical=rows[0].streamCount;
      window.reverseB=queued?{key:rows[1].key,channelId:rows[1].channelId,streamCount:rows[1].streamCount}:null;
      mode=outcome==='saved'?'gateway':'before';receiptMode='busy';window.a=decideMatch(keys[0],'confirmed');window.b=queued?decideMatch(keys[1],'denied'):Promise.resolve();
      return {physical:reversePhysical};
     },{outcome,queued});assert.ok(setup.physical>1,'physical count regression needs a real multi-member group');
     await page.waitForFunction(()=>receiptReads===1&&matchActionState(keys[0]).state==='uncertain');
     await page.evaluate(async policy=>{
      await fetch('/test/exclude?id='+encodeURIComponent(other),{method:'POST'});draft.channels.find(c=>c.id===other).included=false;
      invalidateChangedMatchInputs();window.readPolicy=policy;
     },readPolicy);
     const pending=await page.evaluate(()=>({cloned:matchReview.candidates.find(c=>c.key===keys[0])!==initialProposal,pending:pendingDecisions.size,protected:unloadProtected(),posts:postBodies.length,uncertain:matchActionState(keys[0]).state}));
     assert.equal(pending.cloned,true);assert.equal(pending.protected,true);assert.equal(pending.posts,1);assert.equal(pending.uncertain,'uncertain');
     await page.evaluate(async()=>{receiptMode='real';mode='normal';await recoverMatchActions();await Promise.all([a,b])});
     // Wait until the deliberately held/failed current read has actually started;
     // it must not be able to heal the local state before these assertions.
     await page.waitForFunction(()=>authoritativeReads.length>0);
     const result=await page.evaluate(({outcome,queued})=>{
      const eligibleB=queued&&draft.channels.some(c=>c.id===reverseB.channelId&&c.included);
      return {action:matchActionState(keys[0]).state,rows:matchReview.candidates.map(c=>({key:c.key,error:c.saveError||'',streamCount:c.streamCount})),groups:matchReview.candidateCount,physical:matchReview.candidateStreamCount,historyA:matchReview.decisions.filter(d=>d.key===keys[0]).length,confirmed:matchReview.confirmedCount,posts:postBodies.length,pending:pendingDecisions.size,protected:unloadProtected(),expectedGroups:(outcome==='not-saved'?1:0)+(eligibleB?1:0),expectedPhysical:(outcome==='not-saved'?reversePhysical:0)+(eligibleB?reverseB.streamCount:0),a:keys[0]};
     },{outcome,queued});
     assert.equal(result.action,outcome==='saved'?'saved':'failed');assert.equal(result.groups,result.expectedGroups);assert.equal(result.physical,result.expectedPhysical);
     assert.equal(result.rows.length,result.expectedGroups);assert.equal(result.rows.reduce((n,c)=>n+c.streamCount,0),result.expectedPhysical);assert.equal(result.confirmed,outcome==='saved'?1:0);assert.equal(result.historyA,outcome==='saved'?1:0);
     const aRows=result.rows.filter(c=>c.key===result.a);assert.equal(aRows.length,outcome==='saved'?0:1);if(aRows.length)assert.doesNotMatch(aRows[0].error,/Save not yet verified|Save uncertain/);
     assert.equal(result.posts,1);assert.equal(result.pending,0);assert.equal(result.protected,false);
     assert.equal(await page.locator('#match-candidates').innerText(),String(result.expectedGroups));assert.equal(await page.locator('#match-confirmed').innerText(),outcome==='saved'?'1':'0');
     const disk=await page.evaluate(async()=> (await fetch('/test/inspection')).json());assert.equal(disk[kind],outcome==='saved'?setup.physical:0);
     console.log('REVERSE_RECEIPT '+JSON.stringify({kind,outcome,readPolicy,queued,result}));
     if(process.env.SAVE_RETIREMENT_SCREENSHOT_DIR&&!queued)await page.locator('#match-review').screenshot({path:process.env.SAVE_RETIREMENT_SCREENSHOT_DIR+'/'+kind+'-uncertain-before-edit-'+outcome+'-'+readPolicy+'.png'});
     if(readPolicy==='delay'){
      await page.evaluate(()=>{window.readPolicy='normal';releaseReads.forEach(resolve=>resolve())});
      await page.waitForFunction(()=>authoritativeReads.every(r=>r.finished)&&!dispatchBusy);
     }
     await checkClean();
    }
    // Modal choices use the same real save path. A gateway stays recoverable
    // inside the modal, and view retirement cannot discard a known popup save.
    for(const popupMode of ['gateway','held']){
     await fixture(kind);await page.evaluate(popupMode=>{mode=popupMode;receiptMode=popupMode==='gateway'?'busy':'real';window.primary={...matchReview.candidates[0],key:'popup-primary',alternatives:matchReview.candidates.slice(0,2)};openMatchAlternatives(primary)},popupMode);
     await page.locator('#match-alternative-list .confirm-button').first().click();
     await page.locator('#match-alternative-list .deny-button').nth(1).click();
     await page.waitForFunction(()=>responseReady);
     if(popupMode==='held'){
      await page.evaluate(async()=>{await fetch('/test/exclude?id='+encodeURIComponent(other),{method:'POST'});draft.channels.find(c=>c.id===other).included=false;invalidateChangedMatchInputs()});
      assert.equal(await page.evaluate(()=>matchActionState(keys[0]).state),'saving');assert.equal(await page.evaluate(()=>unloadProtected()),true);await page.evaluate(()=>releaseAck());
      await page.waitForFunction(()=>pendingDecisions.size===0);
      result=await state();assert.equal(result.posts.length,1);assert.equal(result.states[1],'cancelled');assert.equal(result.confirmed,1);assert.ok(result.history.includes(result.posts[0][0]));assert.equal(await page.locator('#match-alternative-dialog').evaluate(n=>n.open),false);
     }else{
      await page.waitForFunction(()=>receiptReads===1);assert.equal(await page.locator('#match-alternative-dialog').getByRole('button',{name:'Check saved state',exact:true}).isVisible(),true);
      await page.evaluate(async()=>{receiptMode='real';mode='normal';await recoverMatchActions()});await page.waitForFunction(()=>pendingDecisions.size===0&&!document.getElementById('match-alternative-dialog').open);
      result=await state();assert.equal(result.posts.length,2);assert.equal(result.confirmed,1);assert.equal(result.denied,1);
     }
     assert.equal((await state()).protected,false);await checkClean();console.log(kind+' actual modal '+popupMode+' pass');
    }
    // After an expired transport, receipt recovery and Undo own the outcome.
    // Its eventually valid old ACK is consumed and cannot restore the choice.
    await fixture(kind);await page.evaluate(()=>{mode='late';fastDeadline=true;receiptMode='busy';window.a=decideMatch(keys[0],'confirmed')});await page.waitForFunction(()=>responseReady);
    await page.evaluate(async()=>{await fetch('/test/exclude?id='+encodeURIComponent(other),{method:'POST'});draft.channels.find(c=>c.id===other).included=false;invalidateChangedMatchInputs()});
    await page.waitForFunction(()=>receiptReads===1&&matchActionState(keys[0]).state==='uncertain');assert.equal(await page.evaluate(()=>matchActionState(keys[0]).job.retired),true);
    await page.evaluate(async()=>{receiptMode='real';mode='normal';await recoverMatchActions();await a});
    await page.waitForFunction(()=>!recoverMatchActions.pending&&!dispatchBusy);await page.evaluate(()=>clearDecision(keys[0]));const undone=await state();assert.equal(undone.confirmed,0);
    await page.evaluate(()=>releaseAck());await page.waitForTimeout(20);result=await state();assert.equal(result.confirmed,0);assert.equal(result.posts.length,1);assert.ok(!result.history.includes(result.posts[0][0]));await checkClean();
   }
   console.log('SAVE_RETIREMENT_COMPLETE: 56 primary/supplemental gateway, retirement, reverse-receipt, source/target/read/Undo/unload schedules passed');
  }finally{await browser.close()}
 })().catch(e=>{console.error(e);process.exitCode=1});
}
