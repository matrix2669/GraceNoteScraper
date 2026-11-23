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
