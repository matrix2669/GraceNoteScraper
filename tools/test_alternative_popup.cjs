const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const html = fs.readFileSync('lineuparr.html','utf8');
const script = html.split('<script>')[1].split('</script>')[0];
new vm.Script(script);
const line = script.split('\n').find(x => x.includes('const alternatives = candidate.minimumScore'));
assert.ok(line);
for (const decision of ['confirmed','denied']) {
  for (const minimumScore of [94,95,98]) {
    const context = {decision,candidate:{minimumScore,alternatives:[{key:'other'}]}};
    vm.runInNewContext(line+'; result=alternatives.length;',context);
    assert.equal(context.result, minimumScore < 95 ? 1 : 0);
  }
}
const alternate = script.split('async function decideAlternative(')[1].split('function closeMatchAlternatives')[0];
assert.ok(!alternate.includes('closeMatchAlternatives()'));
assert.ok(alternate.includes('card.remove()'));
assert.ok(!script.includes("alternativeDialog.addEventListener('click', event"));
console.log('Alternative popup threshold and multi-confirm checks passed');

(async () => {
  for (const decision of ['confirmed','denied']) {
    const target = {key:'b',streamCount:2};
    const survivor = {key:'c',alternatives:[target]};
    const primary = {alias:'Network',alternatives:[target,survivor]};
    const context = {
      pendingDecisions:new Set(),
      matchReview:{candidates:[target,survivor],candidateCount:2,candidateStreamCount:4,confirmedCount:0,deniedCount:0,decisions:[]},
      api:async()=>{},renderMatchReview:()=>{},reloadDraft:async()=>{},showMessage:()=>{},
      els:{alternativeList:{querySelector:()=>true}},
      primary,target,decision,card:{querySelectorAll:()=>[],remove:()=>{}}
    };
    await vm.runInNewContext('async function decideAlternative('+alternate+';decideAlternative(primary,target,decision,card)',context);
    assert.equal(context.matchReview.candidates.length,1);
    assert.equal(context.matchReview.candidates[0].key,'c');
    assert.equal(context.matchReview.candidateCount,1);
    assert.equal(context.matchReview.candidateStreamCount,2);
    assert.equal(survivor.alternatives.length,0);
    assert.equal(primary.alternatives.length,1);
  }
  console.log('One-to-many alternative decisions preserve other queued targets and pair counts');
})().catch(error=>{console.error(error);process.exitCode=1});
