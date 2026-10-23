const {chromium}=require('playwright');
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});try{
 const page=await browser.newPage({viewport:{width:1450,height:1100}}),errors=[];page.on('pageerror',e=>errors.push(e.message));await page.route('**/*',r=>r.abort());
 const html=fs.readFileSync('lineuparr.html','utf8');await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
 const part=(a,b)=>html.slice(html.indexOf(a),html.indexOf(b,html.indexOf(a)));
 const code=require('./review_fixture_helpers.cjs')(html)+part('    let supplementalImpactRequest =','    function renderAutomaticExclusions(')+part('    function mergeSupplementalReview(', '    async function clearDecision(');
 const item=(key,origin,name,raw,targetRegion,streamRegion)=>({key,origin,channelId:key,channelNumber:'902',channelName:'RAWID',channelDisplayName:name,channelCallSign:'SHORT',alias:raw,streamNames:[raw],streamCount:1,m3uAccountIds:[3],minimumScore:99,maximumScore:99,tier:'exact',targetFeed:{region:targetRegion,provenanceKind:'verified'},streamFeeds:[{name:raw,region:streamRegion}],exactAliases:origin?[]:['HBO 2'],retrievalDescriptors:origin?[name]:[]});
 const signature={...item('signature','','HBO Drama','US: HBO SIGNATURE HD','east','unknown'),channelNumber:'904',channelCallSign:'HBODRHD',minimumScore:100,maximumScore:100,exactAliases:['HBO Signature East'],streamCount:4,streamNames:['PRIME| HBO SIGNATURE ᴿᴬᵂ','US: HBO SIGNATURE HD'],streamFeeds:Array.from({length:4},()=>({region:'unknown'})),reviewWarnings:['USER-ATTESTED provenance should stay out of the card'],identityConflicts:['Backend identity paragraph']};
 const fixture={reviewItems:[signature,
 {...signature,key:'drama-west',channelNumber:'905',targetFeed:{region:'west'},channelCallSign:'HBODHDP',exactAliases:['HBO Signature West']},
 {...signature,key:'movies-east',channelNumber:'910',channelDisplayName:'HBO Movies',channelCallSign:'HBOMVHD',alias:'US: HBO ZONE HD',exactAliases:['HBO Zone East']},
 {...signature,key:'movies-west',channelNumber:'911',channelDisplayName:'HBO Movies',channelCallSign:'HBOMHDP',alias:'TV| HBO MOVIES ᴿᴬᵂ',targetFeed:{region:'west'},exactAliases:['HBO Zone West']},
 item('known-west','supplemental-v1','HBO Drama','US: HBO SIGNATURE WEST HD','west','west'),
 {...item('fuzzy','','HBO Drama','US: HBO DRMA HD','east','unknown'),tier:'fuzzy',exactAliases:[],minimumScore:83,maximumScore:83,reasons:['Lineuparr fuzzy (83)']},
 {...item('no-alias','','HBO Drama','HBODRHD','east','unknown'),exactAliases:[],reasons:['Lineuparr exact']},
 {...item('error','','HBO Drama','US: HBO SIGNATURE HD','east','unknown'),exactAliases:['HBO Signature'],saveError:'Save not acknowledged. Retry this choice.'}],reviewedItems:[],candidateCount:8,candidateStreamCount:15,confirmedCount:0,deniedCount:0,streamCount:15,reviewStatus:'current',supplementalImpact:{state:'idle'},supplemental:{complete:true}};
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
  assert.doesNotMatch(text,/Target feed:|Feed unknown|Identity evidence|Provider name conflicts|user-attested|USER-ATTESTED/);
  const region=row.targetFeed?.region;if(region==='west')assert.match(await card.locator('.match-name').nth(1).innerText(),new RegExp(region,'i'));
  if((row.streamFeeds||[]).every(f=>f.region==='unknown'))assert.doesNotMatch(text,/Stream feed:/);
  if(row.origin)assert.doesNotMatch(text,/\d+%/);else assert.ok(await card.locator('.deny-button').isVisible(),'>=95 Deny stays visible');
  assert.equal(await card.locator('details[open]').count(),0);
  for(const label of ['Confirm','Deny'])assert.ok(await card.getByRole('button',{name:label,exact:true}).isVisible());
 }
 assert.deepEqual(await page.evaluate(()=>['East Carolina TV','EastEnders','BBC East'].map(channelDisplayName=>reviewTargetName({channelDisplayName,targetFeed:{region:'east'}}))),['East Carolina TV','EastEnders','BBC East']);
 assert.equal(await page.evaluate(()=>reviewTargetName({channelDisplayName:'Paramount+ with SHOWTIME - East',targetFeed:{region:'unknown'}})),'Paramount+ with SHOWTIME - East');
 const first=page.locator('article[data-key="signature"]');
 assert.match(await first.innerText(),/Matched alias: HBO Signature East → HBO Drama\./);
 assert.equal(await first.locator('.match-name').nth(1).innerText(),'904 · HBO Drama (HBODRHD)');
 assert.match(await first.innerText(),/100%/);assert.match(await first.innerText(),/4 streams/);
 assert.doesNotMatch(await first.innerText(),/Stream feed:/);
 for(const [key,label]of [['drama-west','905 · HBO Drama West (HBODHDP)'],['movies-east','910 · HBO Movies (HBOMVHD)'],['movies-west','911 · HBO Movies West (HBOMHDP)']])assert.equal(await page.locator(`article[data-key="${key}"] .match-name`).nth(1).innerText(),label);
 assert.match(await page.locator('article[data-key="fuzzy"]').innerText(),/Matched by name similarity/);
 assert.match(await page.locator('article[data-key="no-alias"]').innerText(),/Matched by exact name/);
 assert.match(await page.locator('article[data-key="known-west"]').innerText(),/Name evidence:.*HBO Drama West/);
 assert.match(await page.locator('article[data-key="known-west"]').innerText(),/Stream feed: West/);
 assert.match(await page.locator('article[data-key="error"]').innerText(),/Save not acknowledged/);
 await first.locator('summary').click();assert.match(await first.innerText(),/PRIME\| HBO SIGNATURE ᴿᴬᵂ/);await first.locator('summary').click();
 if(process.env.UNIFIED_SCREENSHOT_DIR){
  const dir=process.env.UNIFIED_SCREENSHOT_DIR;fs.mkdirSync(dir,{recursive:true});
  const groups={hbo:['HBO Hits','HBO Drama','HBO Movies'],cinemax:['Cinemax Hits','Cinemax - West','Cinemáx'],showtime:['Showtime 2','Showtime Extreme','Paramount+ with SHOWTIME - West'],news12:['News 12','News 12 Plus']};
  for(const [name,labels]of Object.entries(groups)){
   await page.evaluate(labels=>{const rows=fixture.reviewItems.filter(r=>labels.includes(r.channelDisplayName));matchReview={...mergeSupplementalReview(fixture),candidates:rows,candidateCount:rows.length};renderMatchReview();},labels);
   await page.locator('#match-review').screenshot({path:path.join(dir,`${name}.png`)});
  }
  await page.evaluate(()=>{matchReview=mergeSupplementalReview(fixture);renderMatchReview();});
  await page.locator('#match-list').screenshot({path:path.join(dir,'all-cards.png')});
  fs.writeFileSync(path.join(dir,'visible-cards.json'),JSON.stringify(await cards.allInnerTexts(),null,2));
  fs.writeFileSync(path.join(dir,'rendered-examples.json'),JSON.stringify(await page.evaluate(()=>fixture.reviewItems.map(r=>({key:r.key,origin:r.origin||'primary',name:r.channelDisplayName,callsign:r.channelCallSign,raw:r.alias,targetFeed:r.targetFeed,streamFeeds:r.streamFeeds,conflicts:r.identityConflicts||[]}))),null,2),{mode:0o600});
 }
 assert.deepEqual(errors,[]);console.log(`Unified rendered browser: ${fixture.reviewItems.length} actual DTO cards; one list, score-free retrieval, readable names, prominent target feeds, raw stream names, concise attribution, >=95 Deny and bottom exclusions passed`);
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
