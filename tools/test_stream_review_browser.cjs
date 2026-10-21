const {chromium}=require('playwright');
const fs=require('node:fs'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
 try {
  const page=await browser.newPage();await page.route('**/*',r=>r.abort());
  const html=fs.readFileSync('lineuparr.html','utf8');
  await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
  const code=html.slice(html.indexOf('    function renderMatchReview('),html.indexOf('    async function clearDecision('));
  await page.evaluate(code=>{
   window.els={};for(const [key,id] of Object.entries({matchReview:'match-review',matchStreams:'match-streams',matchCandidates:'match-candidates',matchConfirmed:'match-confirmed',matchConfirmedJump:'match-confirmed-jump',matchDenied:'match-denied',matchList:'match-list',dispatchLoading:'dispatch-loading',matchStatus:'match-status',decisionList:'decision-list',decisionHistory:'decision-history',alternativeDialog:'match-alternative-dialog',alternativeList:'match-alternative-list',alternativeTitle:'match-alternative-title',alternativeHelp:'match-alternative-help'}))els[key]=document.getElementById(id);
   window.pendingDecisions=new Set();window.dispatchBusy=false;window.decisionRevision=0;window.dispatchReviewEpoch=0;window.workflowRemote={};window.matchReviewLimit=100;
   for(const name of ['renderAutomaticExclusions','renderMatchCoverage','renderGuidedWorkflow','scheduleWorkflowRefresh','showMessage'])window[name]=()=>{};
   window.emptyMessage=text=>{const p=document.createElement('p');p.textContent=text;return p};
   window.reloadDraft=async()=>{};window.fail=false;window.api=async()=>{if(fail)throw Error('save failed')};
   window.candidate=(key,kind='inclusion')=>({key,reviewKind:kind,alias:key,streamNames:[key],streamCount:1,minimumScore:kind==='qualified'?100:82,maximumScore:kind==='qualified'?100:82,channelNumber:'1',channelName:'Fixture',reasons:[],alternatives:[]});
   window.optional=candidate('qualified','qualified');window.next=candidate('second');window.loads=0;
   window.matchReview={streamCount:4,candidateCount:3,requiredCount:2,qualifiedCount:1,confirmedCount:0,deniedCount:0,candidates:[candidate('first'),optional],decisions:[]};
   window.loadMatchReview=async()=>{loads++;matchReview={...matchReview,candidates:[next,optional]};renderMatchReview();};
   (0,eval)(code+'\nwindow.renderMatchReview=renderMatchReview;window.openMatchAlternatives=openMatchAlternatives;');renderMatchReview();
  },code);
  assert.equal(await page.locator('.qualified-matches .confirm-button').count(),0);
  await page.locator('#match-list > .match-card .confirm-button').click();
  await page.waitForFunction(()=>loads===1);
  assert.equal(await page.locator('#match-list > .match-card .match-name').first().textContent(),'second');
  await page.locator('#match-list > .match-card .deny-button').click();
  assert.equal(await page.evaluate(()=>loads),1); // exhausted required queue: no loop
  for(const action of ['Confirm this match','Deny']) {
   await page.evaluate(()=>{const alt=candidate('alternate');matchReview={...matchReview,candidates:[alt],candidateCount:1,requiredCount:1};window.primary=candidate('primary');primary.alternatives=[alt];openMatchAlternatives(primary);fail=true;});
   await page.getByRole('button',{name:action,exact:true}).last().click();
   assert.equal(await page.locator('#match-alternative-dialog').evaluate(n=>n.open),true);
   await page.evaluate(()=>{fail=false});
   await page.getByRole('button',{name:action,exact:true}).last().click();
   await page.waitForFunction(()=>!document.getElementById('match-alternative-dialog').open);
  }
  console.log('Browser: qualified optional Deny, automatic refill, exhaustion and popup Confirm/Deny success/failure pass');
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
