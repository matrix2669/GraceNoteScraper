const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});try{
 const page=await browser.newPage({viewport:{width:1450,height:1100}}),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.route('**/*',r=>r.abort());
 const html=fs.readFileSync('lineuparr.html','utf8');await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
 const part=(a,b)=>html.slice(html.indexOf(a),html.indexOf(b,html.indexOf(a)));
 const code=require('./review_fixture_helpers.cjs')(html)+part('    let supplementalImpactRequest =','    function renderAutomaticExclusions(')+part('    function mergeSupplementalReview(', '    async function clearDecision(');
 const item=(key,origin,name,raw,targetRegion,streamRegion)=>({key,origin,channelId:key,channelNumber:'902',channelName:'RAWID',channelDisplayName:name,channelCallSign:'SHORT',alias:raw,streamNames:[raw],streamCount:1,m3uAccountIds:[3],minimumScore:99,maximumScore:99,tier:'exact',targetFeed:{region:targetRegion,provenanceKind:'verified'},streamFeeds:[{name:raw,region:streamRegion}],exactAliases:origin?[]:['HBO 2'],retrievalDescriptors:origin?[name]:[]});
 const fixture=process.env.UNIFIED_RENDER_FIXTURE?JSON.parse(fs.readFileSync(process.env.UNIFIED_RENDER_FIXTURE,'utf8')):{reviewItems:[item('east','','HBO Hits','US: HBO 2 HD','east','unknown'),item('west','supplemental-v1','HBO Hits','US: HBO 2 WEST HD','west','west'),{...item('news','supplemental-v1','News 12','US: NEWS 12 HD','unknown','unknown'),identityConflicts:['Provider name conflicts: Conflicting synthetic provider name']}],reviewedItems:[],candidateCount:3,candidateStreamCount:3,confirmedCount:0,deniedCount:0,streamCount:3,reviewStatus:'current',supplementalImpact:{state:'idle'},supplemental:{complete:true}};
 await page.evaluate(({code,fixture})=>{
  window.els={};for(const [key,id]of Object.entries({matchReview:'match-review',matchStreams:'match-streams',matchCandidates:'match-candidates',matchConfirmed:'match-confirmed',matchConfirmedJump:'match-confirmed-jump',matchDenied:'match-denied',matchList:'match-list',dispatchLoading:'dispatch-loading',matchStatus:'match-status',decisionList:'decision-list',decisionHistory:'decision-history',alternativeDialog:'match-alternative-dialog',alternativeList:'match-alternative-list'}))els[key]=document.getElementById(id);
  Object.assign(window,{draft:{sourceFingerprint:'isolated',channels:[]},pendingDecisions:new Set(),builderResetPending:false,decisionSaveQueue:Promise.resolve(),matchReconcileTimer:null,pendingAlternativeReviews:[],dispatchBusy:false,decisionRevision:0,dispatchReviewEpoch:0,workflowRemote:{},matchReviewLimit:100});
  for(const name of ['renderAutomaticExclusions','renderMatchCoverage','renderGuidedWorkflow','scheduleWorkflowRefresh','showMessage','setDispatchControls'])window[name]=()=>{};
  window.emptyMessage=text=>{const p=document.createElement('p');p.textContent=text;return p};
  (0,eval)(code+'\nwindow.renderMatchReview=renderMatchReview;window.mergeSupplementalReview=mergeSupplementalReview;window.fixture=undefined;');
  window.fixture=fixture;window.matchReview=mergeSupplementalReview(fixture);renderMatchReview();
 },{code,fixture});
 const cards=page.locator('#match-list>.match-card');assert.equal(await cards.count(),fixture.reviewItems.length);
 const copy=await page.locator('#match-list').innerText();assert.doesNotMatch(copy,/Supplemental|Retrieval rank|Manual feed review required|attribution unavailable|undefined|NaN/);
 assert.equal(await page.locator('#supplemental-impact-check').count(),0);
 assert.equal(await page.locator('#match-list>h4').count(),0);
 assert.ok(await page.evaluate(()=>!!(document.getElementById('decision-history').compareDocumentPosition(document.getElementById('automatic-exclusions'))&Node.DOCUMENT_POSITION_FOLLOWING)),'automatic exclusions are below reviewed matches');
 for(const row of fixture.reviewItems){
  const card=page.locator(`[data-key="${row.key}"]`).first();const text=await card.innerText();
  assert.ok(text.includes(row.alias));assert.ok(text.includes(row.channelDisplayName||row.channelName));
  assert.ok(text.includes('Target feed:'));assert.ok(text.includes('Stream feed:'));
  if(row.origin)assert.doesNotMatch(text,/\d+%/);else assert.ok(await card.locator('.deny-button').isVisible(),'>=95 Deny stays visible');
  for(const conflict of row.identityConflicts||[])assert.ok(text.includes(conflict));
 }
 if(process.env.UNIFIED_SCREENSHOT_DIR){
  const dir=process.env.UNIFIED_SCREENSHOT_DIR;fs.mkdirSync(dir,{recursive:true});
  const groups={hbo:['HBO Hits'],cinemax:['Cinemax Hits','Cinemax - West','Cinemáx'],showtime:['Showtime 2','Showtime Extreme','Paramount+ with SHOWTIME - West'],news12:['News 12','News 12 Plus']};
  for(const [name,labels]of Object.entries(groups)){
   await page.evaluate(labels=>{const rows=fixture.reviewItems.filter(r=>labels.includes(r.channelDisplayName));matchReview={...mergeSupplementalReview(fixture),candidates:rows,candidateCount:rows.length};renderMatchReview();},labels);
   await page.locator('#match-review').screenshot({path:path.join(dir,`${name}.png`)});
  }
  fs.writeFileSync(path.join(dir,'rendered-examples.json'),JSON.stringify(await page.evaluate(()=>fixture.reviewItems.map(r=>({key:r.key,origin:r.origin||'primary',name:r.channelDisplayName,callsign:r.channelCallSign,raw:r.alias,targetFeed:r.targetFeed,streamFeeds:r.streamFeeds,conflicts:r.identityConflicts||[]}))),null,2),{mode:0o600});
 }
 assert.deepEqual(errors,[]);console.log(`Unified rendered browser: ${fixture.reviewItems.length} actual DTO cards; one list, score-free retrieval, readable names, separate target/raw feeds, conflicts, >=95 Deny and bottom exclusions passed`);
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
