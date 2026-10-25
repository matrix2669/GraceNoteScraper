const {chromium}=require('playwright');
const fs=require('node:fs'),assert=require('node:assert/strict');
const html=fs.readFileSync('lineuparr.html','utf8');
const part=(a,b)=>{const i=html.indexOf(a),j=html.indexOf(b,i);assert.ok(i>=0&&j>i,a);return html.slice(i,j)};
const code=part('    const els = {','    let draft = null;')
 +part('    function showMessage(','    async function api(')
 +part('    let builderResetBackup','    function setProviderAddressStatus')
 +part('    function invalidateDispatchReview()','    async function loadDispatchConfig()')
 +part('    async function loadMatchReview(','    function renderAutomaticExclusions(')
 +part('    function renderAutomaticExclusions(','    function setDispatchControls()')
 +part('    function setDispatchControls()','    function showConfirmedMatches()')
 +part('    let draftRequest =','    let matchInputSignature =')
 +part('    function renderSummary()','    function remainingDuplicateSuggestions()')
 +part('    function sameArray(','    async function copyText(')
 +part('    function addCopyURL(','    document.addEventListener(\'DOMContentLoaded\'')
 +part('    async function generateLineup()','    els.exportOpen.addEventListener')
 +part('    async function saveChannel(','    function updateDuplicateReviewCount()');
let complete=false;
process.on('beforeExit',()=>{if(!complete){console.error('INCOMPLETE publication lifecycle');process.exitCode=1}});
const watchdog=setTimeout(()=>{console.error('WATCHDOG publication lifecycle');process.exit(1)},process.env.PUBLICATION_LIFECYCLE_PROBE==='watchdog'?100:45000);
(async()=>{
 if(process.env.PUBLICATION_LIFECYCLE_PROBE){if(process.env.PUBLICATION_LIFECYCLE_PROBE==='beforeExit')clearTimeout(watchdog);await new Promise(()=>{});return;}
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
 try {
  const page=await browser.newPage();await page.route('**/*',r=>r.abort());const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.setContent('<base href="http://fixture/">'+html.replace(/<script>[\s\S]*?<\/script>/g,''));
  await page.evaluate(()=>document.body.insertAdjacentHTML('beforeend','<div id="published-export-summary"></div><p id="export-review-status"></p><p id="supplemental-publication-notice" hidden></p>'));
  await page.evaluate(code=>{
   Object.assign(window,{draft:null,saving:false,publishing:false,pendingChannelSaves:0,pendingDecisions:new Set(),dispatchConfig:{configured:true},dispatchBusy:false,dispatchBusyRequest:0,dispatchConnectionError:false,decisionRevision:0,dispatchReviewEpoch:0,matchReviewRequest:0,matchReviewLimit:100,matchReview:null,workflowProgress:{},workflowRemote:{},reviewOrder:new Map(),localExportChanges:new Set(),savedExportDownload:null,matchInputSignature:null,matchReconcileTimer:null,pendingAlternativeReviews:[],decisionSaveQueue:Promise.resolve(),selectedChannels:new Set()});
   window.makeDraft=(source='A',generation=1)=>({sourceFingerprint:source,resetGeneration:generation,providerName:source,postalCode:'10001',lineupId:source,channels:[{id:'c',name:'Channel',number:'1',included:true,category:'Movies',aliases:[]}],exportSignatures:{included:source+'-current',aliases:'base',categories:'Movies'}});
   window.saved=(id='P1',source='A')=>({exists:true,path:`/${source}.json`,filename:`${source}.json`,publishedAt:id,signatures:{included:source+'-current',aliases:'base',categories:'Movies'}});
   window.impact=(id,state='complete',extras=true)=>({id,fingerprint:'inputs-'+id,jsonHash:'json-'+id,inventoryHash:'inventory',consumerRevision:'opaque-revision',state,unresolved:extras,rows:extras?[{channelNumber:'1',channelName:'Channel',extra:['Extra '+id]}]:[]});
   window.review=impact=>({reviewStatus:'current',streamCount:2,candidateCount:0,candidateStreamCount:0,confirmedCount:0,deniedCount:0,candidates:[],decisions:[],decisionTokens:{},supplementalImpact:impact});
   window.nextDraft=makeDraft();window.summary=saved();window.nextReview=review(impact('J1'));window.publishMode='hold';window.resetFailure=false;window.summaryFailure=false;window.summaryQueue=[];window.calls=[];
   window.api=async(path,options={})=>{
    calls.push({path,method:options.method||'GET',body:options.body?JSON.parse(options.body):null});
    if(path==='/api/lineuparr/publish')return await new Promise((resolve,reject)=>{window.ack=resolve;window.nack=reject});
    if(path==='/api/lineuparr/export-summary'){if(summaryFailure)throw Error('synthetic summary unavailable');if(summaryQueue.length){const f=summaryQueue.shift();return await f();}return summary;}
    if(path==='/api/setup/share-links')return {internalBaseURL:'http://container:8080'};
    if(path==='/api/lineuparr/draft')return structuredClone(nextDraft);
    if(path.startsWith('/api/lineuparr/dispatcharr/review'))return structuredClone(nextReview);
    if(path==='/api/lineuparr/dispatcharr/supplemental-impact'){if(window.holdImpact)return await new Promise(resolve=>window.releaseImpact=resolve);return structuredClone(nextReview.supplementalImpact);}
    if(path==='/api/lineuparr/channel'){nextDraft={...draft,exportSignatures:{...draft.exportSignatures,categories:'edited'}};return {categoryReview:{}};}
    if(path==='/api/lineuparr/dispatcharr/decision')return {saved:true};
    if(path==='/api/lineuparr/reset'){
     if(options.method==='POST'){if(resetFailure)throw Error('synthetic reset rejected');return {backup:'backup',restoreAvailable:true};}
     return {sourceFingerprint:draft?.sourceFingerprint,token:'token',clears:['choices'],preserves:['guide'],recovery:{id:'backup'}};
    }
    throw Error('Unexpected '+path);
   };
   // Only unrelated layout/enrichment and duplicate helpers are synthetic. The
   // production review handlers, controls, messages, signatures and dirty renderer run.
   for(const name of ['renderSources','renderCategoryOptions','renderViewMode','renderBatchControls','renderChannels','renderCategoryReview','renderGuidedWorkflow','scheduleWorkflowRefresh','invalidateChangedMatchInputs','renderMatchCoverage'])window[name]=()=>{};
   window.activeDuplicateGroups=()=>[];window.sourceLabel=x=>x;window.badge=(t)=>{const n=document.createElement('span');n.textContent=t;return n};
   window.emptyMessage=t=>{const n=document.createElement('p');n.textContent=t;return n};
   (0,eval)(code+`\nwindow.reloadDraft=reloadDraft;window.renderAll=renderAll;window.loadMatchReview=loadMatchReview;window.renderMatchReview=renderMatchReview;window.renderSupplementalImpact=renderSupplementalImpact;window.refreshExportSummary=refreshExportSummary;window.generateLineup=generateLineup;window.resetBuilder=resetBuilder;window.saveChannel=saveChannel;window.renderSummary=renderSummary;window.setDispatchControls=setDispatchControls;window.decideMatch=decideMatch;window.stopTimers=()=>{clearTimeout(validationPollTimer);clearTimeout(matchReconcileTimer)};window.state=()=>({view:publicationViewSnapshot(),publication:observedPublication,summary:publicationSummaryState,observations:[...validationObservations.keys()],retired:[...consumedSupplementalObservations],editRevision:localEditRevision,signatures:draft.exportSignatures,dirty:[...localExportChanges],message:els.message.textContent,notice:document.getElementById('supplemental-publication-notice').textContent,noticeHidden:document.getElementById('supplemental-publication-notice').hidden});`);
  },code);
  const notice=page.locator('#supplemental-publication-notice'),dirty=page.locator('.export-stale');
  const ingest=async(id,state='complete',extras=true)=>page.evaluate(async({id,state,extras})=>{nextReview=review(impact(id,state,extras));await loadMatchReview(false,false);}, {id,state,extras});
  const refresh=()=>page.evaluate(()=>refreshExportSummary());
  const start=()=>page.evaluate(()=>{window.publishPromise=generateLineup()});
  const success=async(id='P2',source='A')=>{await page.evaluate(({id,source})=>ack(saved(id,source)),{id,source});await page.evaluate(()=>publishPromise)};
  const replace=async(source,generation=1)=>{await page.evaluate(async({source,generation})=>{nextDraft=makeDraft(source,generation);await reloadDraft();},{source,generation});};
  await replace('A');await refresh();await ingest('J1');assert.equal(await notice.isVisible(),true);
  const registered=await page.evaluate(()=>state().observations);
  await page.evaluate(()=>{renderMatchReview();renderAll();renderMatchReview()});await refresh();
  assert.equal(await notice.isVisible(),true);assert.deepEqual(await page.evaluate(()=>state().observations),registered,'ordinary renders are registration-free');
  await start();await success();assert.equal(await page.getByText('http://container:8080/A.json',{exact:true}).count(),1,'generation preserves the configured Docker-network URL');await ingest('J1');await page.evaluate(()=>renderMatchReview());assert.equal(await notice.isVisible(),false,'captured J1 remains retired on duplicate ingress and rendering');
  console.log('PASS ordinary-rerender, captured-J1 and duplicate-J1');
  await ingest('J-before');await start();await ingest('J-during');assert.equal(await notice.isVisible(),true);await success('P3');assert.equal(await notice.isVisible(),true,'J2 received during held ACK stays actionable');
  await ingest('J-during');assert.equal(await notice.isVisible(),true);await ingest('J-after');assert.equal(await notice.isVisible(),true);
  await ingest('J-resolved','complete',false);assert.equal(await notice.isVisible(),false);await ingest('J-after');assert.equal(await notice.isVisible(),false,'duplicate retired observation cannot resurrect after resolution');await ingest('J-fresh');assert.equal(await notice.isVisible(),true);
  await page.evaluate(()=>{summary=saved('unknown-order-summary');});await refresh();assert.equal(await notice.isVisible(),true,'summary-only publication change cannot consume unresolved findings');
  console.log('PASS held-ACK chronology, duplicate/later J2, resolution/rearm, unknown-order summary');
  await page.evaluate(()=>{window.holdImpact=true;scheduleValidationPoll()});await page.waitForFunction(()=>!!window.releaseImpact);
  await ingest('J-newer-shared-read');await page.evaluate(()=>{holdImpact=false;releaseImpact(impact('old-resolved-read','complete',false));});await page.waitForTimeout(30);
  assert.equal(await notice.isVisible(),true,'earlier validation read cannot retire newer accepted observations');
  assert.equal(await page.evaluate(()=>state().observations.some(key=>key.includes('old-resolved-read'))),false,'rejected older read is never registered');
  console.log('PASS shared validation read ownership across poll/review ingress');
  for(const uncertain of [false,true]){
   await page.evaluate(()=>{localExportChanges.add('aliases');renderSummary()});await refresh();const before=await page.evaluate(()=>state());const count=await page.evaluate(()=>calls.filter(c=>c.path.endsWith('/publish')).length);
   await start();await page.evaluate(uncertain=>{const e=Error('synthetic definitive publication rejection');e.uncertain=uncertain;nack(e);},uncertain);await page.evaluate(()=>publishPromise);
   const after=await page.evaluate(()=>state());assert.deepEqual(after.signatures,before.signatures);assert.deepEqual(after.dirty,before.dirty);assert.deepEqual(after.retired,before.retired);assert.equal(await notice.isVisible(),true);assert.equal(await dirty.count(),1);assert.equal(await page.locator('#export-open').isEnabled(),true);
   assert.match(after.message,uncertain?/not acknowledged/:/definitive publication rejection/);assert.equal(await page.evaluate(()=>calls.filter(c=>c.path.endsWith('/publish')).length),count+1,'no automatic retry');
  }
  console.log('PASS definitive/unconfirmed failure preserves findings, signatures, dirty state and eligibility');
  for(const outcome of ['success','failure']){
   await page.evaluate(()=>{summaryFailure=false;summary=saved('A-start');});await replace('A');await refresh();await ingest('old-A-'+outcome);await start();
   await page.evaluate(()=>summaryFailure=true);await replace('B');await page.evaluate(()=>{localExportChanges.add('aliases');document.getElementById('message').textContent='B current message';});await refresh();const before=await page.evaluate(()=>state());
   if(outcome==='success')await success('A-late');else {await page.evaluate(()=>nack(Error('A late failure')));await page.evaluate(()=>publishPromise)}
   const after=await page.evaluate(()=>state());for(const key of ['view','publication','signatures','dirty','message','observations','retired'])assert.deepEqual(after[key],before[key],`stale A ${outcome} mutated B ${key}`);assert.equal(after.summary,'failed');assert.equal(after.noticeHidden,true);
  }
  console.log('PASS actual A-to-B late success/failure with failed B summary');
  await page.evaluate(()=>{summaryFailure=false;summary=saved('A-before-ABA');});await replace('A');await refresh();const first=await page.evaluate(()=>state().view);await start();await replace('B');await replace('A');const second=await page.evaluate(()=>state().view);assert.notEqual(first.viewEpoch,second.viewEpoch);await page.evaluate(()=>document.getElementById('message').textContent='A new incarnation');await success('A-old-ABA');assert.equal(await page.locator('#message').textContent(),'A new incarnation');
  console.log('PASS actual A-to-B-to-A incarnation fence');
  for(const resetFailure of [false,true]){
   await page.evaluate(resetFailure=>{window.resetFailure=resetFailure;summary=saved('before-reset');nextDraft={...draft};},resetFailure);await refresh();await ingest('reset-J-'+resetFailure);const before=await page.evaluate(()=>state().view);await start();
   await page.evaluate(()=>{window.resetPromise=resetBuilder()});await page.locator('#builder-reset-dialog').waitFor({state:'visible'});await page.locator('#builder-reset-confirm').click();await page.evaluate(()=>resetPromise);const after=await page.evaluate(()=>state().view);
   assert.equal(after.viewEpoch===before.viewEpoch,resetFailure,'only successful actual reset advances incarnation, even when same-source/generation is returned');
   if(resetFailure){await success('failed-reset-still-owned');assert.match(await page.locator('#message').textContent(),/Lineup file generated/);}else{const message=await page.locator('#message').textContent();await success('late-before-successful-reset');assert.equal(await page.locator('#message').textContent(),message);}
  }
  console.log('PASS same-source successful reset and failed-reset ownership');
  await page.evaluate(()=>{resetFailure=false;summary=saved('before-edit');});await refresh();await ingest('edit-before');await start();
  await page.evaluate(async()=>{await saveChannel(draft.channels[0],{category:'News'},document.createElement('input'));await reloadDraft();});
  const edited=await page.evaluate(()=>state());await success('before-edit-ack');const afterEdit=await page.evaluate(()=>state());assert.deepEqual(afterEdit.signatures,edited.signatures);assert.deepEqual(afterEdit.dirty,edited.dirty);assert.ok(await dirty.count());assert.match(await dirty.textContent(),/Channel categories have changed/);
  // Hold an earlier summary across a newer publication and a newer summary read.
  await page.evaluate(()=>{summaryQueue.push(()=>new Promise(resolve=>window.releaseSummary=resolve));window.oldSummaryPromise=refreshExportSummary()});await start();await success('newer-publication');await page.evaluate(()=>releaseSummary(saved('stale-summary')));await page.evaluate(()=>oldSummaryPromise);assert.equal(await page.evaluate(()=>state().publication.record.publishedAt),'newer-publication');
  await page.evaluate(()=>{summaryQueue.push(()=>new Promise(resolve=>window.releaseSummary=resolve));window.oldSummaryPromise=refreshExportSummary()});await page.evaluate(()=>summary=saved('newer-summary'));await refresh();await page.evaluate(()=>releaseSummary(saved('stale-older-read')));await page.evaluate(()=>oldSummaryPromise);assert.equal(await page.evaluate(()=>state().publication.record.publishedAt),'newer-summary');
  console.log('PASS actual channel edit during publication retains signatures/dirty indicator; summary read fences');
  await ingest('absence-extra');await page.evaluate(()=>{summary={exists:false};});await refresh();assert.equal(await notice.isVisible(),false);assert.match(await page.locator('#published-export-summary').textContent(),/No lineup file/);await page.evaluate(()=>summaryFailure=true);await refresh();assert.equal(await notice.isVisible(),false);assert.match(await page.locator('#published-export-summary').textContent(),/Could not confirm/);
  await page.evaluate(()=>{summaryFailure=false;summary=saved('after-absence')});await refresh();assert.equal(await notice.isVisible(),true,'absence preserves actionable observations, restored publication supplies context');
  for(const metadata of [undefined,null,{state:'failed'}]){await page.evaluate(async metadata=>{nextReview=review(metadata);await loadMatchReview(false,false);},metadata);assert.match(await page.locator('#supplemental-impact-status').textContent(),/unavailable/);assert.equal(await page.locator('#match-reload').isVisible(),true);assert.equal(await page.locator('#export-open').isEnabled(),true);}
  console.log('PASS authoritative absence vs failed summary; missing/null/failed metadata recovery');
  await page.evaluate(()=>stopTimers());assert.deepEqual(errors,[]);complete=true;console.log('PUBLICATION_LIFECYCLE_COMPLETE all acceptance schedules finished');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1}).finally(()=>clearTimeout(watchdog));
