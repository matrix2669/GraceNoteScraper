const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');
const html = fs.readFileSync(path.join(__dirname, '../lineuparr.html'), 'utf8');
for (const match of html.matchAll(/<script[^>]*>([\s\S]*?)<\/script>/g)) new vm.Script(match[1]);
const start = html.indexOf('    let matchInputSignature = null;');
const end = html.indexOf('    function renderSummary()', start);
assert(start > 0 && end > start);
const context = vm.createContext({
  draft:{channels:[{id:'one',included:true,name:'One',number:'1'},{id:'two',included:true,name:'Two',number:'2'}]},
  matchReview:{candidates:[{channelId:'one',streamCount:1,alternatives:[{channelId:'two'}]},{channelId:'two',streamCount:1}],candidateCount:2,streamCount:10},
  matchReviewRequest:0, dispatchReviewEpoch:0, workflowRemote:{}, closeMatchAlternatives(){}, renderMatchReview(){},
});
vm.runInContext(html.slice(start,end),context);
vm.runInContext('invalidateChangedMatchInputs()',context);
context.draft.channels[1].included=false;
vm.runInContext('invalidateChangedMatchInputs()',context);
assert.equal(context.matchReview.candidates.length,1);
assert.equal(context.matchReview.candidates[0].alternatives.length,0);
assert.equal(context.matchReview.streamCount,10);
context.draft.channels[1].included=true;
vm.runInContext('invalidateChangedMatchInputs()',context);
assert.equal(context.matchReview.candidates.length,0);
assert.equal(context.matchReview.streamCount,0);
assert.match(context.matchReview.warning,/Refresh required/);
console.log('Snapshot UI: removal reuse, addition invalidation, and syntax passed');

async function testProgress() {
  const begin = html.indexOf('    function startMatchProgress()');
  const finish = html.indexOf('    async function loadMatchReview(', begin);
  let scheduled;
  let state = {stage:'matching', completed:1, total:4};
  const elements = {
    matchProgress:{hidden:true}, matchProgressLabel:{textContent:''},
    matchProgressBar:{value:undefined, removeAttribute() { this.value = undefined; }},
  };
  const progressContext = vm.createContext({
    els:elements, AbortController,
    setTimeout(fn) { scheduled = fn; return 1; }, clearTimeout() { scheduled = null; },
    async fetch() { return {ok:true, async json() { return state; }}; },
  });
  vm.runInContext(html.slice(begin,finish), progressContext);
  const stop = vm.runInContext('startMatchProgress()', progressContext);
  assert.equal(elements.matchProgress.hidden,false);
  assert.equal(elements.matchProgressBar.value,undefined);
  await scheduled();
  assert.equal(elements.matchProgressBar.value,25);
  assert.match(elements.matchProgressLabel.textContent,/1 of 4/);
  state = {stage:'finishing'};
  await scheduled();
  assert.equal(elements.matchProgressBar.value,100);
  stop();
  assert.equal(elements.matchProgress.hidden,true);
  assert.equal(scheduled,null);
  assert.match(html, /finally \{\s+stopProgress\(\)/);
  console.log('Progress UI: real percentage, finishing state, and cleanup passed');
}
testProgress().catch(error => { console.error(error); process.exitCode = 1; });

async function testWorkflowCompletionAndLateReplies() {
  const a=html.indexOf('    function workflowStates()');
  const b=html.indexOf('    function renderGuidedWorkflow()',a);
  const w=vm.createContext({draft:{channels:[]},workflowRemote:{dispatchReady:true,dispatchConfig:{configured:true}},workflowProgress:{},dispatchBusy:false,dispatchConnectionError:false,pendingDecisions:new Set(),document:{getElementById:()=>({}),querySelector:()=>({})}});
  vm.runInContext(require('../tools/review_fixture_helpers.cjs')(html)+html.slice(a,b),w);
  for(const [status,count,expected] of [['refresh-required',0,false],['stale',0,false],['refreshing',0,false],['current',0,true],['current',2,false]]) {
    w.workflowRemote.dispatch={reviewStatus:status,candidateCount:count,...(status==='current'&&count===0?{candidates:[]}:{}),supplementalImpact:{state:'idle'}};assert.equal(vm.runInContext('workflowStates().find(s=>s.step===6).complete',w),expected);
  }
  // High scores remain unresolved review proposals even if an obsolete optional-only counter says zero.
  w.workflowRemote.dispatch={reviewStatus:'current',candidateCount:1,requiredCount:0,qualifiedCount:1,candidates:[{reviewKind:'qualified',score:100}]};
  assert.equal(vm.runInContext('workflowStates().find(s=>s.step===6).complete',w),false);
  w.workflowRemote.dispatch={reviewStatus:'current',candidateCount:0};w.dispatchBusy=true;
  assert.equal(vm.runInContext('workflowStates().find(s=>s.step===6).complete',w),false);
  w.dispatchBusy=false;w.dispatchConnectionError=true;
  assert.equal(vm.runInContext('workflowStates().find(s=>s.step===6).complete',w),false);
  // Execute real loadMatchReview with controllable deferred replies.
  const start=html.indexOf('    async function loadMatchReview('),end=html.indexOf('    function renderMatchReview()',start);
  let resolve,reject;const messages=[];
  const c=vm.createContext({document:{getElementById:()=>({textContent:'',replaceChildren(){},append(){}}),createElement:()=>({textContent:''})},builderResetPending:false,draft:{},dispatchConfig:{configured:true},dispatchBusy:false,dispatchBusyRequest:0,dispatchReviewEpoch:0,matchReviewRequest:0,decisionRevision:0,pendingDecisions:new Set(),pendingAlternativeReviews:[],matchReconcileTimer:null,clearTimeout(){},matchReviewLimit:100,matchReview:null,workflowRemote:{},URLSearchParams,
    els:{dispatchLoading:{replaceChildren(){}}},emptyMessage:x=>x,setDispatchControls(){},renderGuidedWorkflow(){},renderMatchReview(){},renderAutomaticExclusions(){},startMatchProgress:()=>()=>{},showMessage:x=>messages.push(x),api:()=>new Promise((yes,no)=>{resolve=yes;reject=no})});
  vm.runInContext(html.slice(start,end),c);
  let pending=vm.runInContext('loadMatchReview(false,false)',c);c.dispatchReviewEpoch++;c.matchReviewRequest++;resolve({reviewStatus:'current',candidateCount:0});await pending;assert.equal(c.matchReview,null);
  pending=vm.runInContext('loadMatchReview(false,false)',c);c.dispatchReviewEpoch++;c.matchReviewRequest++;reject(new Error('obsolete'));await pending;assert.equal(messages.length,0);
  pending=vm.runInContext('loadMatchReview(true)',c);assert.equal(c.dispatchBusy,true);c.dispatchReviewEpoch++;c.matchReviewRequest++;resolve({reviewStatus:'current',candidateCount:0});await pending;assert.equal(c.dispatchBusy,false);assert.equal(c.matchReview,null);
  c.matchReview={reviewStatus:'current',candidateCount:0};pending=vm.runInContext('loadMatchReview(true)',c);reject(new Error('offline'));await pending;assert.equal(c.matchReview.reviewStatus,'stale');
  // A connection change fences review and workflow replies together.
  const i=html.indexOf('    function invalidateDispatchReview()'),j=html.indexOf('    async function loadDispatchConfig()',i);
  vm.runInContext(html.slice(i,j),c);vm.runInContext('invalidateDispatchReview()',c);assert.equal(c.matchReview,null);assert.equal(c.workflowRemote.dispatch,null);assert.equal(c.workflowRemote.dispatchReady,false);
  // A pre-refresh workflow poll cannot reinstate old current/zero completion.
  const pollStart=html.indexOf('    async function loadWorkflowStatus()'),pollEnd=html.indexOf('    function workflowStates()',pollStart);
  let oldPoll,refresh;
  c.workflowRefreshing=false;c.workflowProgress={};c.workflowRemote={};c.dispatchBusy=false;
  c.document.querySelector=selector=>selector==='#dispatch-panel'?{}:null;
  c.scheduleWorkflowRefresh=()=>{};
  c.optionalAPI=path=>c.api(path).catch(()=>null);
  c.api=path=> {
    if(path==='/api/lineuparr/workflow')return Promise.resolve({});
    if(path==='/api/lineuparr/dispatcharr/config')return Promise.resolve({configured:true});
    return new Promise(resolve=>{if(path.includes('refresh=true'))refresh=resolve;else oldPoll=resolve;});
  };
  vm.runInContext(html.slice(pollStart,pollEnd),c);
  const polling=vm.runInContext('loadWorkflowStatus()',c);
  for(let i=0;i<10&&!oldPoll;i++)await Promise.resolve();assert(oldPoll);
  const refreshing=vm.runInContext('loadMatchReview(true)',c);assert(refresh);
  refresh({reviewStatus:'stale',candidateCount:0});await refreshing;
  oldPoll({reviewStatus:'current',candidateCount:0});await polling;
  assert.notEqual(c.workflowRemote.dispatch?.reviewStatus,'current');
  // Poll begun during Save/Test must not reuse the previously configured snapshot.
  c.renderDispatchConfig=()=>{};
  for(const name of ['dispatchURL','dispatchAuth','dispatchUser','dispatchPassword','dispatchAPIKey'])c.els[name]={value:'synthetic'};
  let saved;oldPoll=null;c.matchReview=null;c.workflowRemote={};c.workflowRefreshing=false;
  c.api=(path,options)=> {
    if(options?.method==='POST')return new Promise(resolve=>saved=resolve);
    if(path==='/api/lineuparr/workflow')return Promise.resolve({});
    if(path==='/api/lineuparr/dispatcharr/config')return Promise.resolve({configured:true});
    if(path.endsWith('limit=1'))return new Promise(resolve=>oldPoll=resolve);
    return Promise.resolve({reviewStatus:'refresh-required',candidateCount:0});
  };
  const saveStart=html.indexOf('    async function saveDispatch('),saveEnd=html.indexOf('    async function disconnectDispatch()',saveStart);
  vm.runInContext(html.slice(saveStart,saveEnd),c);
  const saving=vm.runInContext('saveDispatch({preventDefault(){}})',c);
  const during=vm.runInContext('loadWorkflowStatus()',c);
  for(let i=0;i<10&&!oldPoll;i++)await Promise.resolve();assert(oldPoll);
  saved({configured:true});await saving;oldPoll({reviewStatus:'current',candidateCount:0});await during;
  assert.equal(c.workflowRemote.dispatch.reviewStatus,'refresh-required');
  // The predicate cannot complete optimistic decisions still being saved.
  w.dispatchConnectionError=false;w.pendingDecisions.add('pending');
  assert.equal(vm.runInContext('workflowStates().find(s=>s.step===6).complete',w),false);
  console.log('Workflow UI: current-only completion, empty-valid status, loading/error and late-response fencing passed');
}
testWorkflowCompletionAndLateReplies().catch(error=>{console.error(error);process.exitCode=1});
