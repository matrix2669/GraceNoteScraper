const {chromium}=require('playwright');
const fs=require('node:fs'),assert=require('node:assert/strict');
const html=fs.readFileSync('lineuparr.html','utf8');
const part=(a,b)=>{const i=html.indexOf(a),j=html.indexOf(b,i);assert.ok(i>=0&&j>i,a);return html.slice(i,j)};
const code=part('    const els = {','    let draft = null;')
 +part('    function showMessage(','    let builderResetBackup')
 +part('    function workflowStates()','    function renderGuidedWorkflow()')
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
process.on('beforeExit',()=>{if(!complete){console.error('INCOMPLETE publication certainty');process.exitCode=1}});
const watchdog=setTimeout(()=>{console.error('WATCHDOG publication certainty');process.exit(1)},process.env.PUBLICATION_CERTAINTY_PROBE==='watchdog'?100:45000);
(async()=>{
 if(process.env.PUBLICATION_CERTAINTY_PROBE){if(process.env.PUBLICATION_CERTAINTY_PROBE==='beforeExit')clearTimeout(watchdog);await new Promise(()=>{});return;}
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
   window.saved=(id='known',source='A')=>({exists:true,path:`/${source}/${id}.json`,filename:`${id}.json`,publishedAt:'2026-10-25T12:00:00Z',signatures:{included:source+'-current',aliases:'base',categories:'Movies'}});
   window.summary=saved();window.transportMode='hold';window.commitFirst=false;window.publicationPosts=0;
   window.workflowRemote.dispatchConfig={configured:true};
   window.fetch=async(path,options={})=>{
    calls.push({path,method:options.method||'GET',body:options.body?JSON.parse(options.body):null});
    if(path==='/api/lineuparr/publish'){
     publicationPosts++;
     if(commitFirst) summary=saved('committed-'+transportMode,draft.sourceFingerprint);
     if(transportMode==='network')throw new TypeError('Synthetic response lost after request arrival');
     if(transportMode==='malformed')return new Response('{',{status:200,headers:{'Content-Type':'application/json'}});
     if(transportMode==='success-body-loss')return new Response(new ReadableStream({start(c){c.error(new TypeError('Synthetic success body lost'));}}),{status:200,headers:{'Content-Type':'application/json'}});
     if(transportMode==='error-body-loss')return new Response(new ReadableStream({start(c){c.error(new TypeError('Synthetic error body lost'));}}),{status:502});
     if(transportMode==='http502')return new Response('Bad Gateway',{status:502});
     if(transportMode==='invalid-ack')return Response.json({unrecognized:'not a publication ACK'});
     if(transportMode==='definitive')return new Response('Synthetic publication rejected',{status:409});
     return Response.json(await new Promise((resolve,reject)=>{window.ack=resolve;window.nack=reject}));
    }
    if(path==='/api/lineuparr/export-summary'){
     if(summaryFailure)throw new TypeError('Synthetic summary unavailable');
     if(summaryQueue.length)return Response.json(await summaryQueue.shift()());
     return Response.json(summary);
    }
    if(path==='/api/setup/share-links')return Response.json({internalBaseURL:'http://container:8080'});
    if(path==='/api/lineuparr/draft')return Response.json(nextDraft);
    if(path.startsWith('/api/lineuparr/dispatcharr/review'))return Response.json(nextReview);
    if(path==='/api/lineuparr/dispatcharr/supplemental-impact'){
     if(window.holdImpact)return Response.json(await new Promise(resolve=>window.releaseImpact=resolve));
     return Response.json(nextReview.supplementalImpact);
    }
    if(path==='/api/lineuparr/channel')return Response.json({categoryReview:{}});
    if(path==='/api/lineuparr/dispatcharr/decision')return Response.json({saved:true});
    if(path==='/api/lineuparr/reset')return Response.json({sourceFingerprint:draft?.sourceFingerprint,token:'token',clears:['choices'],preserves:['guide'],recovery:{id:'backup'}});
    throw Error('Unexpected '+path);
   };
   // Only unrelated layout/enrichment and duplicate helpers are synthetic. The
   // production review handlers, controls, messages, signatures and dirty renderer run.
   for(const name of ['renderSources','renderCategoryOptions','renderViewMode','renderBatchControls','renderChannels','renderCategoryReview','renderGuidedWorkflow','scheduleWorkflowRefresh','invalidateChangedMatchInputs','renderMatchCoverage'])window[name]=()=>{};
   window.activeDuplicateGroups=()=>[];window.sourceLabel=x=>x;window.badge=(t)=>{const n=document.createElement('span');n.textContent=t;return n};
   window.emptyMessage=t=>{const n=document.createElement('p');n.textContent=t;return n};
   (0,eval)(code+`\nwindow.reloadDraft=reloadDraft;window.renderAll=renderAll;window.loadMatchReview=loadMatchReview;window.renderMatchReview=renderMatchReview;window.renderSupplementalImpact=renderSupplementalImpact;window.refreshExportSummary=refreshExportSummary;window.generateLineup=generateLineup;window.resetBuilder=resetBuilder;window.saveChannel=saveChannel;window.renderSummary=renderSummary;window.setDispatchControls=setDispatchControls;window.decideMatch=decideMatch;window.workflowStates=workflowStates;window.stopTimers=()=>{clearTimeout(validationPollTimer);clearTimeout(matchReconcileTimer)};window.state=()=>({view:publicationViewSnapshot(),publication:observedPublication,summary:publicationSummaryState,observations:[...validationObservations.keys()],retired:[...consumedSupplementalObservations],editRevision:localEditRevision,signatures:draft.exportSignatures,dirty:[...localExportChanges],message:els.message.textContent,notice:document.getElementById('supplemental-publication-notice').textContent,noticeHidden:document.getElementById('supplemental-publication-notice').hidden});`);
  },code);
  const notice=page.locator('#supplemental-publication-notice');
  const ingest=async result=>page.evaluate(async result=>{nextReview=review(result);await loadMatchReview(false,false);},result);
  const impact=(id,state='complete',extras=true)=>({id,fingerprint:'inputs-'+id,jsonHash:'json-'+id,inventoryHash:'inventory',consumerRevision:'opaque-revision',state,unresolved:extras,rows:extras?[{channelNumber:'1',channelName:'Channel',extra:['Extra '+id]}]:[]});
  const refresh=()=>page.evaluate(()=>refreshExportSummary());
  const start=()=>page.evaluate(()=>{window.publishPromise=generateLineup()});
  const replace=async source=>page.evaluate(async source=>{nextDraft=makeDraft(source);await reloadDraft();},source);
  await replace('A');await refresh();
  for(const mode of ['network','malformed','success-body-loss','error-body-loss','http502','invalid-ack']){
   await ingest(impact('clear-'+mode,'complete',false));
   await page.evaluate(mode=>{summaryFailure=false;summary=saved('confirmed-'+mode);transportMode=mode;commitFirst=['network','malformed','http502'].includes(mode);},mode);await refresh();
   await ingest(impact('extra-'+mode));await page.evaluate(()=>{localExportChanges.add('aliases');renderPublishedSnapshot()});
   const before=await page.evaluate(()=>state()),posts=await page.evaluate(()=>publicationPosts);
   // Hold a pre-outcome authoritative read. It must not erase later uncertainty.
   await page.evaluate(()=>{summaryQueue.push(()=>new Promise(resolve=>window.releaseOldSummary=resolve));window.oldSummaryPromise=refreshExportSummary()});
   await start();await page.evaluate(()=>publishPromise);
   const after=await page.evaluate(()=>state());
   assert.equal(after.summary,'unconfirmed',mode+' must mark publication knowledge unconfirmed');
   for(const key of ['publication','signatures','dirty','observations','retired'])assert.deepEqual(after[key],before[key],mode+' preserves '+key);
   assert.match(after.message,/Generation was not acknowledged/);assert.match(after.notice,/Additional matches need review.*unconfirmed/);assert.doesNotMatch(after.notice,/has not changed/);
   assert.match(await page.locator('#published-export-summary').textContent(),/Last confirmed snapshot:.*publication outcome is unconfirmed/);
   assert.equal(await page.locator('.export-stale').count(),1);assert.equal(await page.locator('#export-open').isEnabled(),true);
   assert.equal(await page.evaluate(()=>publicationPosts),posts+1,'no automatic retry for '+mode);
   await page.evaluate(()=>releaseOldSummary(saved('pre-outcome-summary')));await page.evaluate(()=>oldSummaryPromise);assert.equal(await page.evaluate(()=>state().summary),'unconfirmed','older summary cannot erase uncertainty');
   await page.evaluate(()=>{renderMatchReview();renderSummary();renderPublishedSnapshot();summaryFailure=true});await refresh();
   assert.equal(await page.evaluate(()=>state().summary),'unconfirmed','failed summary preserves uncertainty');assert.match(await notice.textContent(),/unconfirmed/);
   await page.evaluate(()=>summaryFailure=false);await refresh();
   assert.equal(await page.evaluate(()=>state().summary),'present','current authoritative summary resolves uncertainty');
   const recovered=await page.evaluate(()=>state());assert.equal(recovered.publication.record.filename,(['network','malformed','http502'].includes(mode)?'committed-':'confirmed-')+mode+'.json');
   assert.deepEqual(recovered.dirty,before.dirty);assert.deepEqual(recovered.signatures,before.signatures);assert.match(recovered.notice,/Additional matches need review/);
   assert.doesNotMatch(await page.locator('#published-export-summary').textContent(),/Last confirmed snapshot:|outcome is unconfirmed/);
   console.log('PASS actual-api '+mode+': persistent uncertainty, last-confirmed snapshot, retained findings/dirty state, stale/failed summary fences, authoritative recovery and one POST');
  }
  await page.evaluate(()=>{transportMode='definitive';commitFirst=false;summary=saved('definitive-base')});await refresh();const definite=await page.evaluate(()=>state());await start();await page.evaluate(()=>publishPromise);
  assert.equal(await page.evaluate(()=>state().summary),'present');assert.deepEqual(await page.evaluate(()=>state().publication),definite.publication);assert.match(await page.locator('#message').textContent(),/Synthetic publication rejected/);assert.match(await notice.textContent(),/has not changed/);
  console.log('PASS actual-api definitive 409 preserves confirmed certainty without retry');
  // Actual transport may persist before loss when no prior snapshot existed.
  await page.evaluate(()=>{summary={exists:false}});await refresh();await page.evaluate(()=>{transportMode='network';commitFirst=true});await start();await page.evaluate(()=>publishPromise);
  assert.equal(await page.evaluate(()=>state().publication),null);assert.match(await page.locator('#published-export-summary').textContent(),/outcome is unconfirmed/);assert.match(await notice.textContent(),/Additional matches need review.*unconfirmed/);
  await page.evaluate(()=>{summary={exists:false}});await refresh();assert.equal(await page.evaluate(()=>state().summary),'absent');assert.equal(await notice.isVisible(),false);
  console.log('PASS uncertain generation without previous snapshot and authoritative absence recovery');
  // A late real-API failure belongs only to its captured A view.
  await page.evaluate(()=>{summary=saved('source-A-known');transportMode='hold';commitFirst=false});await replace('A');await refresh();await ingest(impact('stale-A-extra'));await start();
  await page.evaluate(()=>summaryFailure=true);await replace('B');await refresh();await page.evaluate(()=>{localExportChanges.add('categories');document.getElementById('message').textContent='B current message'});const replacement=await page.evaluate(()=>state());
  await page.evaluate(()=>nack(new TypeError('Synthetic stale A transport loss')));await page.evaluate(()=>publishPromise);assert.deepEqual(await page.evaluate(()=>state()),replacement,'late A network loss cannot change B knowledge, snapshot, notice, signatures, dirty state or message');
  console.log('PASS actual-api stale-source transport failure leaves replacement UI unchanged');
  await page.evaluate(()=>{summaryFailure=false;summary=saved('conflicts-base','B')});await refresh();
  for(const [field,label] of [['protectedConflicts','Saved confirmation conflict'],['missed','Missing match'],['sameNameConflicts','Same-name conflict']]){
   const extra=impact('extra-before-'+field);await ingest(extra);assert.equal(await notice.isVisible(),true);
   const only={...impact('only-'+field,'complete',false),unresolved:true,rows:[{channelNumber:'1',channelName:'Channel',extra:[],[field]:['Unrelated conflict '+field]}]};
   for(const state of ['running','stale','failed']){await ingest({...only,id:'negative-'+state+'-'+field,state});assert.equal(await notice.isVisible(),true,state+' no-extra observation cannot retire extras');}
   await ingest({...only,id:'stale-findings-'+field,findingsStale:true});assert.equal(await notice.isVisible(),true,'stale findings in complete response cannot retire extras');
   await ingest(only);assert.equal(await notice.isVisible(),false,'completed '+field+'-only observation resolves extras');
   assert.match(await page.locator('#supplemental-impact-results').textContent(),new RegExp(label));assert.doesNotMatch(await page.locator('#supplemental-impact-results').textContent(),/Additional match to review/);
   assert.equal(await page.evaluate(()=>workflowStates().find(s=>s.step===6).complete),false,'unrelated conflict remains a completion blocker');
   await ingest(extra);assert.equal(await notice.isVisible(),false,'duplicate old extra identity stays retired');
   await ingest(impact('new-extra-after-'+field));assert.equal(await notice.isVisible(),true,'genuinely new extra rearms');
   await ingest(only);assert.equal(await notice.isVisible(),true,'duplicate old no-extra result cannot retire newer extras');
   console.log('PASS extra-specific resolution: '+field+', unrelated conflict UI/completion, stale/running/failed negatives, duplicate old identities and new extra rearming');
  }
  // Cross-ingress older read with unrelated conflicts may not retire new extras.
  await page.evaluate(()=>{window.holdImpact=true;scheduleValidationPoll()});await page.waitForFunction(()=>!!window.releaseImpact);
  await ingest(impact('newer-read-extra'));await page.evaluate(()=>{holdImpact=false;releaseImpact({...impact('rejected-older-no-extra','complete',false),unresolved:true,rows:[{extra:[],protectedConflicts:['Old conflict']}]});});await page.waitForTimeout(30);
  assert.equal(await notice.isVisible(),true);assert.equal(await page.evaluate(()=>state().observations.some(key=>key.includes('rejected-older-no-extra'))),false);
  console.log('PASS rejected older no-extra read cannot retire newer extras');
  await page.evaluate(()=>stopTimers());assert.deepEqual(errors,[]);complete=true;console.log('PUBLICATION_CERTAINTY_COMPLETE all U2 actual-API and extra-resolution schedules finished');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1}).finally(()=>clearTimeout(watchdog));
