const { chromium } = require('playwright');
const fs = require('node:fs');
const assert = require('node:assert/strict');

const html = fs.readFileSync('lineuparr.html', 'utf8');
const part = (start, end) => html.slice(html.indexOf(start), html.indexOf(end, html.indexOf(start)));
const code = require('./review_fixture_helpers.cjs')(html)
  + part('    let supplementalImpactRequest =', '    function renderAutomaticExclusions(')
  + part('    function workflowStates()', '    function renderGuidedWorkflow()')
  + 'function invalidateChangedMatchInputs() {} function activeDuplicateGroups() { return []; }\n'
  + part('    function renderSummary()', '    function remainingDuplicateSuggestions()')
  + part('    function sameArray(', '    async function copyText(')
  + part('    async function copyText(', "    document.addEventListener('DOMContentLoaded'")
  + part('    let draftRequest =', '    function renderAll()')
  + part('    async function generateLineup()', "    els.exportOpen.addEventListener('click', generateLineup);");

const watchdog = setTimeout(() => { console.error('Export review status fixture timed out'); process.exit(1); }, 30000);
(async () => {
  const browser = await chromium.launch({ headless: true, channel: process.env.PLAYWRIGHT_CHANNEL || undefined });
  try {
    const page = await browser.newPage();
    page.on('pageerror', error => { throw error; });
    await page.setContent(`<base href="http://fixture/"><main>
      <button id="export-open">Generate Lineup File</button><p id="export-review-status" role="status"></p>
      <p id="supplemental-publication-notice" hidden></p><div id="published-export-summary"></div>
      <p id="supplemental-impact-status"></p><div id="supplemental-impact-results"></div>
      <button id="match-reload" hidden>Reload review</button>
      <details id="dispatch-panel"></details><div id="alias-panel"></div><div id="major-market-panel"></div>
      <div id="tmdb-category-panel"></div><div id="category-review-panel"></div><div class="builder-panel"></div>
    </main>`);
    await page.evaluate(code => {
      window.els = {exportOpen:document.getElementById('export-open'), included:{}, total:{}, aliases:{}, categorized:{}, unresolved:{}, removeDuplicates:{}, restoreAll:{}};
      Object.assign(window, {
        draft:{sourceFingerprint:'fixture',channels:[],exportSignatures:{included:'new',rows:{}}},
        matchReview:{reviewStatus:'current',candidateCount:0,candidates:[],supplemental:{complete:true,candidateCount:0},supplementalImpact:{state:'running',unresolved:true,rows:[]}},
        workflowRemote:{dispatchReady:true,dispatchConfig:{configured:true},dispatch:{reviewStatus:'current',candidateCount:0,candidates:[],supplemental:{complete:true,candidateCount:0},supplementalImpact:{state:'running',unresolved:true}}},
        workflowProgress:{tmdbDisposition:'complete'}, pendingDecisions:new Set(), pendingChannelSaves:0, builderResetPending:false,
        dispatchBusy:false, dispatchConnectionError:false, decisionRevision:0, dispatchReviewEpoch:0, saving:false, publishing:false,
        localExportChanges:new Set(), messages:[], savedExportDownload:null, publishCount:0, exportExists:true, draftResponse:null, renderAll:()=>{}, refreshResetRecovery:async()=>{},
        showMessage:(text,error)=>messages.push({text,error}), scheduleValidationPoll:()=>{},
        emptyMessage:text=>{const p=document.createElement('p');p.textContent=text;return p},
        api:async(path,options)=>{
          if(path==='/api/lineuparr/publish') { window.publishCount++;window.publishBody=JSON.parse(options.body); return {path:'/lineup.json',publishedAt:'2026-10-25T00:00:00Z',signatures:{included:'saved',rows:{}}}; }
          if(path==='/api/lineuparr/export-summary') return {exists:window.exportExists,path:'/lineup.json',filename:'lineup.json',publishedAt:'2026-10-25T00:00:00Z',signatures:{included:'saved',rows:{}}};
          if(path==='/api/lineuparr/draft') return window.draftResponse || {...draft};
          if(path==='/api/setup/share-links') return {internalBaseURL:'http://fixture'};
          throw Error('unexpected fixture API '+path);
        }
      });
      (0,eval)(code+'\nwindow.renderSupplementalImpact=renderSupplementalImpact;window.receiveImpact=result=>{const accepted=acceptSupplementalImpact(result);matchReview.supplementalImpact=accepted;renderSupplementalImpact(accepted);};window.renderReviewBoundaryStatus=renderReviewBoundaryStatus;window.workflowStates=workflowStates;window.reloadDraft=reloadDraft;window.renderSummary=renderSummary;window.refreshExportSummary=refreshExportSummary;window.generateLineup=generateLineup;window.clearValidationPoll=()=>clearTimeout(validationPollTimer);');
    }, code);

    const pendingCopy='Checking saved choices…';
    await page.evaluate(()=>receiveImpact({state:'complete',rows:[{extra:['pre-publication extra']}] }));
    assert.equal(await page.locator('#supplemental-publication-notice').isVisible(),false,'extras without an observed publication do not claim it is unchanged');
    await page.evaluate(() => receiveImpact({state:'running',unresolved:true,rows:[{channelNumber:'7',channelName:'Fixture',extra:['already queued extra'],protectedConflicts:[{name:'retained finding'}]}]}));
    assert.equal(await page.locator('#supplemental-impact-status').textContent(),pendingCopy,'empty queue explains active validation despite retained finding');
    assert.doesNotMatch(await page.locator('#supplemental-impact-results').textContent(),/Additional match to review|already queued extra|proposals or use Undo/,'stale or duplicated extras are not a second action surface');
    assert.match(await page.locator('#supplemental-impact-results').textContent(),/Saved confirmation conflict.*retained finding/,'unrelated protected conflicts remain visible');
    assert.match(await page.locator('#supplemental-impact-results').textContent(),/Saved confirmation conflict/);
    assert.equal(await page.locator('#export-review-status').textContent(),'Review checks are still running. Generating now publishes the current saved choices.');
    assert.equal(await page.locator('#export-open').isEnabled(),true,'pending validation alone does not disable generation');
    assert.equal(await page.evaluate(()=>workflowStates().find(step=>step.step===6).complete),false,'Step 6 stays incomplete during validation');
    for (const state of [undefined,null]) {
      const complete=await page.evaluate(state=>{workflowRemote.dispatch={reviewStatus:'current',candidateCount:0,candidates:[],supplemental:{complete:true,candidateCount:0},supplementalImpact:state};matchReview.supplementalImpact=state;renderReviewBoundaryStatus();return workflowStates().find(step=>step.step===6).complete;},state);
      assert.equal(complete,false,'missing/null validation observation cannot certify Step 6');
      assert.match(await page.locator('#export-review-status').textContent(),/Reload review to retry/,'missing/null observation exposes recovery status');
    }
    for (const state of ['queued','preparing','running','failed','stale','unknown']) {
      const complete=await page.evaluate(state=>{workflowRemote.dispatch={reviewStatus:'current',candidateCount:0,candidates:[],supplemental:{complete:true,candidateCount:0},supplementalImpact:{state,unresolved:false}};return workflowStates().find(step=>step.step===6).complete;},state);
      assert.equal(complete,false,`Step 6 stays incomplete for ${state} validation`);
    }
    assert.equal(await page.evaluate(()=>{workflowRemote.dispatch={reviewStatus:'current',candidateCount:0,candidates:[],supplemental:{complete:true,candidateCount:0},supplementalImpact:{state:'complete',unresolved:false}};return workflowStates().find(step=>step.step===6).complete;}),true,'a current complete review with no blockers can complete Step 6');
    assert.equal(await page.evaluate(()=>{workflowRemote.dispatch.supplementalImpact.unresolved=true;return workflowStates().find(step=>step.step===6).complete;}),false,'retained unresolved findings block Step 6');
    assert.equal(await page.evaluate(()=>{workflowRemote.dispatch.supplementalImpact.unresolved=false;workflowRemote.dispatch.candidateCount=1;return workflowStates().find(step=>step.step===6).complete;}),false,'remaining proposals block Step 6');
    await page.evaluate(()=>{workflowRemote.dispatch.candidateCount=0;pendingDecisions.add('step6-pending');});
    assert.equal(await page.evaluate(()=>workflowStates().find(step=>step.step===6).complete),false,'unsettled choices block Step 6');
    await page.evaluate(()=>{pendingDecisions.clear();matchActionState('step6-uncertain','uncertain',{source:'fixture'});});
    assert.equal(await page.evaluate(()=>workflowStates().find(step=>step.step===6).complete),false,'uncertain receipts block Step 6');
    await page.evaluate(()=>matchActionState.states.clear());

    await page.evaluate(() => {matchReview.supplementalImpact={state:'running',unresolved:true,rows:[]};workflowRemote.dispatch.supplementalImpact=matchReview.supplementalImpact;generateLineup();});
    await page.waitForFunction(()=>messages.some(message=>message.text.startsWith('Lineup generated.')));
    assert.equal(await page.evaluate(()=>publishBody.sourceFingerprint),'fixture');
    assert.equal(await page.evaluate(()=>messages.at(-1).text),'Lineup generated. Review checks are still running; additional matches may appear.');
    assert.equal(await page.locator('#export-review-status').textContent(),'Review checks are still running. Generating now publishes the current saved choices.');

    await page.evaluate(() => {const result={id:'J-status-1',state:'complete',rows:[{channelNumber:'8',channelName:'Extra',extra:['New alias']}]};workflowRemote.dispatch={reviewStatus:'current',candidateCount:1,supplemental:{complete:true,candidateCount:1},supplementalImpact:result};receiveImpact(result);});
    const notice='Additional matches need review. The published file has not changed.';
    assert.equal(await page.locator('#supplemental-publication-notice').textContent(),notice);
    assert.equal(await page.locator('#supplemental-publication-notice').isVisible(),true);
    await page.evaluate(()=>{receiveImpact({state:'complete',rows:[]});});
    assert.equal(await page.locator('#supplemental-publication-notice').isVisible(),false,'resolved completed findings retire the notice');
    await page.evaluate(()=>receiveImpact({id:'J-status-2',state:'complete',rows:[{extra:['current extra']}] }));
    assert.equal(await page.locator('#supplemental-publication-notice').isVisible(),true,'current source extras remain actionable against its publication');
    await page.evaluate(async()=>{exportExists=false;await refreshExportSummary();});
    assert.equal(await page.locator('#supplemental-publication-notice').isVisible(),false,'an absent publication retires the notice');
    await page.evaluate(async()=>{exportExists=true;await refreshExportSummary();receiveImpact({id:'J-status-3',state:'complete',rows:[{extra:['extra after restored publication']}]});});
    assert.equal(await page.locator('#supplemental-publication-notice').isVisible(),true,'a newly observed publication scopes later extras');
    await page.evaluate(async()=>{draftResponse={...draft,sourceFingerprint:'replacement-source'};await reloadDraft();receiveImpact({id:'J-old-scope',state:'complete',rows:[{extra:['late old-scope extra']}]});});
    assert.equal(await page.locator('#supplemental-publication-notice').isVisible(),false,'actual draft replacement retires and cannot recreate the old publication notice');
    await page.evaluate(async()=>{await generateLineup();});
    assert.equal(await page.locator('#supplemental-publication-notice').isVisible(),false,'successful regeneration retires the prior publication notice');
    await page.evaluate(()=>receiveImpact({id:'J-status-4',state:'complete',rows:[{extra:['still-unpublished extra']}] }));
    assert.equal(await page.locator('#supplemental-publication-notice').isVisible(),true,'completed extras are actionable against the newly observed publication');
    await page.evaluate(async()=>await generateLineup());
    assert.equal(await page.locator('#supplemental-publication-notice').isVisible(),false,'subsequent successful regeneration retires the notice again');
    await page.evaluate(()=>receiveImpact({id:'J-status-5',state:'complete',rows:[{extra:['latest extra']}] }));
    assert.equal(await page.locator('#supplemental-publication-notice').isVisible(),true,'later completed extras reappear for the current publication');
    assert.equal(await page.evaluate(()=>workflowStates().find(step=>step.step===6).complete),false,'new extras remain a review blocker');
    await page.evaluate(async()=>{localExportChanges.add('included');await refreshExportSummary();renderSummary();});
    assert.equal(await page.locator('#supplemental-publication-notice').textContent(),notice,'summary rerender retains publication notice');
    assert.match(await page.locator('#published-export-summary').textContent(),/Included channels have changed/,'content-change warning remains');

    await page.evaluate(()=>receiveImpact({state:'complete',unresolved:true,rows:[{extra:['hidden extra']}]}));
    assert.equal(await page.locator('#supplemental-impact-results').textContent(),'','hidden extras alone do not recreate generic unresolved prose');
    for (const state of ['idle','complete']) {
      await page.evaluate(state=>receiveImpact({state,rows:[]}),state);
      assert.equal(await page.locator('#supplemental-impact-status').textContent(),'','settled checks clear empty-list pending copy');
    }
    await page.evaluate(()=>receiveImpact(null));
    assert.match(await page.locator('#supplemental-impact-status').textContent(),/unavailable/,'null observation is normalized to unknown');
    await page.evaluate(()=>receiveImpact({state:'running',rows:[]}));
    await page.evaluate(()=>{pendingDecisions.add('queued');receiveImpact(matchReview.supplementalImpact);renderSummary();});
    assert.equal(await page.locator('#supplemental-impact-status').textContent(),'','pending writes keep validation progress separate');
    assert.equal(await page.locator('#export-review-status').textContent(),'Choices are saving; review checks are still running. Generation will be available when choices settle and publishes current saved choices.');
    assert.equal(await page.locator('#export-open').isDisabled(),true,'existing unsettled-save guard remains');
    await page.evaluate(()=>generateLineup());
    assert.equal(await page.evaluate(()=>publishCount),3,'unsettled save still refuses a new publish');
    await page.evaluate(()=>{pendingDecisions.clear();matchActionState('uncertain','uncertain',{source:draft.sourceFingerprint});receiveImpact(matchReview.supplementalImpact);});
    assert.equal(await page.locator('#supplemental-impact-status').textContent(),'','uncertain writes keep receipt recovery primary');
    assert.equal(await page.locator('#export-review-status').textContent(),'A saved choice is still being verified; review checks are still running. Generation will be available when the choice is settled and publishes current saved choices.');
    await page.evaluate(()=>generateLineup());
    assert.equal(await page.evaluate(()=>publishCount),3,'uncertain receipt still refuses a new publish');
    await page.evaluate(()=>matchActionState.states.clear());
    for (const state of ['failed','stale','unknown']) {
      await page.evaluate(state=>receiveImpact({state,rows:[]}),state);
      assert.match(await page.locator('#supplemental-impact-status').textContent(),/unavailable/);
      assert.match(await page.locator('#export-review-status').textContent(),/Reload review to retry/);
    }
    assert.equal(await page.locator('#match-reload').isVisible(),true,'failed and unknown recovery remains exposed');
    assert.deepEqual(await page.evaluate(()=>messages.map(message=>message.error||false)), [false,false,false,false,false,false,false,false], 'generation and guarded retries produce truthful status messages only');
    await page.evaluate(()=>clearValidationPoll());
    console.log('Export review status browser: pending empty-list/findings, qualified generation, persistent extras notice, summary warning preservation, Step 6 and save-recovery states passed');
  } finally { await browser.close(); }
})().catch(error=>{console.error(error);process.exitCode=1;}).finally(()=>clearTimeout(watchdog));
