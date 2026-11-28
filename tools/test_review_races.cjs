const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
let suiteCompleted=false,suiteFailed=false;
const completionWatchdog=setTimeout(()=>{console.error('REVIEW_RACES_INCOMPLETE: watchdog expired');process.exit(1)},10000);completionWatchdog.unref();
process.on('beforeExit',()=>{if(!suiteCompleted&&!suiteFailed){console.error('REVIEW_RACES_INCOMPLETE: exited before final assertions');process.exitCode=1}});
const html=fs.readFileSync('lineuparr.html','utf8');
const slice=(a,b)=>html.slice(html.indexOf(a),html.indexOf(b,html.indexOf(a)));
const code=require('./review_fixture_helpers.cjs')(html)+slice('    function saveMatchDecision(', '    async function clearDecision(')+
 slice('    async function refillResolvedPage(', '    function matchCard(')+
 slice('    let matchInputSignature = null;', '    function renderSummary(')+
 slice('    async function loadMatchReview(', '    function renderAutomaticExclusions(')+
 slice("    els.alternativeDialog.addEventListener('close'", "    document.getElementById('builder-reset').addEventListener");
const tick=async()=>{for(let i=0;i<15;i++)await Promise.resolve()};
function fixture(keys) {
 const candidate=key=>({key,channelId:key,channelName:key,channelNumber:'1',alias:key,streamCount:1,streamNames:[key],minimumScore:100,maximumScore:100,alternatives:[]});
 const c={document:{getElementById:()=>({replaceChildren(){},append(){},setAttribute(){},addEventListener(){}}),querySelectorAll:()=>[]},draft:{sourceFingerprint:'fixture',channels:keys.map(id=>({id,included:true,name:id,number:'1'}))},dispatchConfig:{configured:true},matchReview:{reviewStatus:'current',candidates:keys.map(candidate),candidateCount:keys.length,candidateStreamCount:keys.length,confirmedCount:0,deniedCount:0,decisions:[]},pendingDecisions:new Set(),pendingAlternativeReviews:[],dispatchBusy:false,builderResetPending:false,decisionRevision:0,dispatchReviewEpoch:0,matchReviewRequest:0,dispatchBusyRequest:0,matchReviewLimit:100,decisionSaveQueue:Promise.resolve(),matchReconcileTimer:null,workflowRemote:{},setTimeout(){},clearTimeout(){},URLSearchParams,renderGuidedWorkflow(){},scheduleWorkflowRefresh(){},emptyMessage:x=>x,startMatchProgress:()=>()=>{},showMessage(message){c.messages.push(message)},messages:[],saves:[],reads:[],opened:[],closeEvents:[]};
 let close;
 c.els={alternativeDialog:{dataset:{},open:false,hasAttribute(){return this.open},addEventListener(name,fn){close=fn},close(){this.open=false;c.closeEvents.push(close)}},alternativeList:{querySelector:()=>null},dispatchLoading:{replaceChildren(){}}};
 c.api=(path,options)=>new Promise((resolve,reject)=>{if(options)c.saves.push({body:JSON.parse(options.body),resolve,reject});else c.reads.push({resolve,reject})});
 vm.createContext(c);vm.runInContext(code,c);
 // This VM suite keeps a small DOM boundary but must run the production
 // recovery renderer's lifecycle effects. The separate browser/Go retirement
 // suite runs the complete production renderMatchReview and actual handlers.
 c.renderMatchReview=()=>{c.renderMatchRecovery();c.status=c.pendingDecisions.size?'saving':'settled'};
 c.setDispatchControls=()=>{c.disabled=c.dispatchBusy||c.pendingDecisions.size>0};
 c.openMatchAlternatives=primary=>{c.opened.push(primary);c.els.alternativeDialog.open=true;c.els.alternativeDialog.dataset.reviewKey=primary.key};
 c.reloadDraft=async()=>{};
 c.card=()=>({querySelectorAll:()=>[],remove(){}});
 c.candidate=candidate;
 return c;
}
(async()=>{
 // R1: real removal-only invalidation preserves optimistic history. It must
 // remove the unsent entry, restore a still-applicable row, and unlock controls.
 const c=fixture(['A','B','other']);c.invalidateChangedMatchInputs();
 const a=c.decideMatch('A','confirmed'),b=c.decideMatch('B','denied');await tick();
 c.draft.channels[2].included=false;c.invalidateChangedMatchInputs();
 c.saves[0].resolve({saved:true});await Promise.all([a,b]);
 assert.equal(c.saves.length,1);assert.deepEqual(Array.from(c.matchReview.decisions,d=>d.key),['A']);
 assert.deepEqual(Array.from(c.matchReview.candidates,d=>d.key),['B']);assert.equal(c.pendingDecisions.size,0);assert.equal(c.disabled,false);assert.equal(c.status,'settled');assert.match(c.messages.at(-1),/not acknowledged.*B/);
 // A replacement choice with the same display key owns its own row, history
 // and pending marker. An old job completion cannot remove or settle it.
 const newer=fixture(['A']);const oldChoice=newer.decideMatch('A','confirmed');await tick();
 const newOwner={id:99999,source:'fixture',key:'A',epoch:newer.dispatchReviewEpoch};
 const newRow={...newer.candidate('A'),reviewDecisionOwner:newOwner};const newHistory={key:'A',decision:'denied',reviewDecisionOwner:newOwner};
 newer.matchReview={...newer.matchReview,candidates:[newRow],decisions:[newHistory],candidateCount:1,candidateStreamCount:1,confirmedCount:0,deniedCount:1};
 newer.matchActionState('A','saving',{operation:'replacement-operation',job:{proposalOwner:newOwner,source:'fixture',epoch:newer.dispatchReviewEpoch,scope:newer.matchReview.operationScope,sent:true,terminal:false}});
 newer.saves[0].resolve({saved:true});await oldChoice;
 assert.strictEqual(newer.matchReview.candidates[0],newRow);assert.strictEqual(newer.matchReview.decisions[0],newHistory);assert.equal(newer.matchReview.deniedCount,1);assert.equal(newer.matchActionState('A').operation,'replacement-operation');assert.equal(newer.pendingDecisions.has('A'),true);
 newer.pendingDecisions.clear();newer.matchActionState('A').job.terminal=true;
 // R1 follow-up: a restored fuzzy group cannot reintroduce an excluded
 // alternative target, including when the restored primary is retried.
 const g=fixture(['A','B','other']);g.matchReview.candidates[1].minimumScore=82;g.matchReview.candidates[1].alternatives=[g.candidate('other')];g.invalidateChangedMatchInputs();
 const ga=g.decideMatch('A','confirmed'),gb=g.decideMatch('B','denied');await tick();g.draft.channels[2].included=false;g.invalidateChangedMatchInputs();g.saves[0].resolve({saved:true});await Promise.all([ga,gb]);
 assert.equal(g.matchReview.candidates[0].alternatives.length,0);const retry=g.decideMatch('B','confirmed');await tick();g.saves[1].resolve({saved:true});await retry;assert.equal(g.opened.length,0);
 // R3 follow-up: a new choice made during old-epoch cancellation must retain
 // the pending rows' page ranks, not reconstruct order from visible rows alone.
 const h=fixture(['A','B','C','other']);h.invalidateChangedMatchInputs();const ha=h.decideMatch('A','confirmed'),hb=h.decideMatch('B','denied');await tick();h.draft.channels[3].included=false;h.invalidateChangedMatchInputs();const hc=h.decideMatch('C','denied');h.saves[0].resolve({saved:true});await tick();assert.equal(h.saves.length,2);h.saves[1].reject(Error('C failed'));await Promise.all([ha,hb,hc]);assert.deepEqual(Array.from(h.matchReview.candidates,c=>c.key),['B','C']);assert.deepEqual(Array.from(h.matchReview.decisions,d=>d.key),['A']);
 // R3: failure order cannot change the original visible ordering.
 const d=fixture(['A','B','C']);const pb=d.decideMatch('B','denied'),pa=d.decideMatch('A','confirmed');await tick();
 d.saves[0].reject(Error('B failed'));await tick();d.saves[1].reject(Error('A failed'));await Promise.all([pa,pb]);
 assert.deepEqual(Array.from(d.matchReview.candidates,c=>c.key),['A','B','C']);assert.equal(d.matchReview.decisions.length,0);
 // R2: successful sub-95 choices leave alternate pairs in the ordinary queue.
 const e=fixture(['P1','P2','P3','alt-P1','alt-P2','alt-P3','next']);
 for(const p of e.matchReview.candidates.slice(0,3)){p.minimumScore=82;p.maximumScore=82;p.alternatives=[e.candidate('alt-'+p.key)]}
 const saves=['P1','P2','P3'].map(key=>e.decideMatch(key,'confirmed'));await tick();
 for(let i=0;i<3;i++){e.saves[i].resolve({saved:true});await tick()}await Promise.all(saves);
 assert.equal(e.opened.length,0,'successful primary choices never open the alternatives dialog');
 assert.equal(e.pendingAlternativeReviews.length,0);
 for(const key of ['alt-P1','alt-P2','alt-P3']) assert.ok(e.matchReview.candidates.some(c=>c.key===key),key+' remains an ordinary review card');
 const alternateSaves=['alt-P1','alt-P2','alt-P3'].map((key,i)=>e.decideMatch(key,i===1?'denied':'confirmed'));await tick();
 for(let i=0;i<3;i++){e.saves[3+i].resolve({saved:true});await tick()}await Promise.all(alternateSaves);
 assert.equal(e.opened.length,0);assert.equal(e.pendingAlternativeReviews.length,0);assert.equal(e.reads.length,0,'remaining ordinary card prevents an unnecessary refill read');
 assert.deepEqual(Array.from(e.matchReview.candidates,c=>c.key),['next']);
 // A failed background read started before a decision cannot mark the newer
 // optimistic review stale or show an unrelated error.
 const f=fixture(['A']);const old=f.loadMatchReview(false,false);const choice=f.decideMatch('A','confirmed');await tick();f.reads[0].reject(Error('obsolete read'));await old;
 assert.equal(f.matchReview.reviewStatus,'current');assert.equal(f.messages.length,0);f.saves[0].resolve({saved:true});await choice;
 suiteCompleted=true;clearTimeout(completionWatchdog);
 console.log('REVIEW_RACES_COMPLETE: unsent-choice rollback, replacement ownership, alternate-target exclusion, stable failed ordering, ordinary alternate cards, and stale-read rejection passed');
})().catch(error=>{suiteFailed=true;clearTimeout(completionWatchdog);console.error(error);process.exitCode=1});
