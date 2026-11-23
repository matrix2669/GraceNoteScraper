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
  matchReviewRequest:0, closeMatchAlternatives(){}, renderMatchReview(){},
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
