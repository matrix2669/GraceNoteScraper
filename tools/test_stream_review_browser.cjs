const {chromium}=require('playwright');
const fs=require('node:fs'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
 try {
  const page=await browser.newPage();await page.route('**/*',r=>r.abort());
  const html=fs.readFileSync('lineuparr.html','utf8');
  await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
  const code=require('./review_fixture_helpers.cjs')(html)+html.slice(html.indexOf('    function renderMatchReview('),html.indexOf('    async function clearDecision('));
  await page.evaluate(code=>{
   window.els={};for(const [key,id] of Object.entries({matchReview:'match-review',matchStreams:'match-streams',matchCandidates:'match-candidates',matchConfirmed:'match-confirmed',matchConfirmedJump:'match-confirmed-jump',matchDenied:'match-denied',matchList:'match-list',dispatchLoading:'dispatch-loading',matchStatus:'match-status',decisionList:'decision-list',decisionHistory:'decision-history',alternativeDialog:'match-alternative-dialog',alternativeList:'match-alternative-list',alternativeTitle:'match-alternative-title',alternativeHelp:'match-alternative-help'}))els[key]=document.getElementById(id);
   window.draft={sourceFingerprint:'fixture',channels:[]};window.pendingDecisions=new Set();window.builderResetPending=false;window.decisionSaveQueue=Promise.resolve();window.matchReconcileTimer=null;window.pendingAlternativeReviews=[];window.setDispatchControls=()=>{};window.dispatchBusy=false;window.decisionRevision=0;window.dispatchReviewEpoch=0;window.workflowRemote={};window.matchReviewLimit=100;
   for(const name of ['renderAutomaticExclusions','renderMatchCoverage','renderGuidedWorkflow','scheduleWorkflowRefresh','showMessage'])window[name]=()=>{};
   window.emptyMessage=text=>{const p=document.createElement('p');p.textContent=text;return p};
   window.reloadDraft=async()=>{};window.fail=false;window.writes=[];window.api=async(path,options)=>{if(fail)throw Error('save failed');writes.push(JSON.parse(options.body))};
   window.candidate=(key,kind='inclusion')=>({key,reviewKind:kind,alias:key,streamNames:[key],streamCount:1,minimumScore:kind==='qualified'?100:82,maximumScore:kind==='qualified'?100:82,channelNumber:'1',channelName:'Fixture',reasons:[],alternatives:[]});
   window.high=candidate('high','qualified');window.next=candidate('boundary','qualified');next.minimumScore=next.maximumScore=95;window.loads=0;
   window.matchReview={streamCount:4,candidateCount:3,confirmedCount:0,deniedCount:0,candidates:[high,candidate('low')],decisions:[]};
   window.loadMatchReview=async()=>{loads++;matchReview={...matchReview,candidates:[next]};renderMatchReview();};
   (0,eval)(code+'\nwindow.renderMatchReview=renderMatchReview;window.openMatchAlternatives=openMatchAlternatives;');renderMatchReview();
  },code);
  const row=key=>page.locator(`#match-list > .match-card[data-key="${key}"]`);
  assert.equal(await page.locator('#match-list > .match-card').count(),2);
  assert.equal(await page.locator('.qualified-matches').count(),0);
  assert.equal(await row('high').isVisible(),true);
  assert.equal(await row('high').locator('.confirm-button').isVisible(),true);
  assert.equal(await row('high').locator('.deny-button').isVisible(),true);
  // Clearing a low-score item must neither hide nor skip the high-score item.
  await row('low').locator('.confirm-button').click();
  assert.equal(await page.evaluate(()=>loads),0);
  assert.equal(await row('high').isVisible(),true);
  await row('high').locator('.deny-button').click();
  await page.waitForFunction(()=>loads===1);
  assert.equal(await row('boundary').isVisible(),true);
  await row('boundary').locator('.confirm-button').click();
  assert.equal(await page.evaluate(()=>loads),1);
  assert.equal(await page.evaluate(()=>matchReview.candidateCount),0);
  assert.deepEqual(await page.evaluate(()=>writes.map(w=>[w.key,w.decision])),[['low','confirmed'],['high','denied'],['boundary','confirmed']]);
  // Every score class has the same visible popup review controls. A failed save
  // keeps the item retryable; a successful final decision closes the popup.
  for(const kind of ['inclusion','qualified','feed-review']) for(const action of ['Confirm this match','Deny']) {
   await page.evaluate(kind=>{const alt=candidate('alternate',kind);matchReview={...matchReview,candidates:[alt],candidateCount:1};window.primary=candidate('primary');primary.alternatives=[alt];openMatchAlternatives(primary);fail=true;},kind);
   const popup=page.locator('#match-alternative-dialog');
   assert.equal(await popup.getByRole('button',{name:'Confirm this match',exact:true}).isVisible(),true);
   assert.equal(await popup.getByRole('button',{name:'Deny',exact:true}).isVisible(),true);
   await popup.getByRole('button',{name:action,exact:true}).click();
   assert.equal(await popup.evaluate(n=>n.open),true);
   assert.equal(await page.evaluate(()=>matchReview.candidateCount),1);
   await page.evaluate(()=>{fail=false});
   await popup.getByRole('button',{name:action,exact:true}).click();
   await page.waitForFunction(()=>!document.getElementById('match-alternative-dialog').open);
  }
  console.log('Browser: all-score visible Confirm/Deny, 95 boundary, full-list automatic refill, exhaustion and popup success/failure pass');
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
