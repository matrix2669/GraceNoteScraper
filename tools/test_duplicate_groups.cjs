const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict');
const html=fs.readFileSync('lineuparr.html','utf8'), script=html.split('<script>')[1].split('</script>')[0];
new vm.Script(script);
function declaration(name) {
 const start=script.indexOf('    function '+name+'('), rest=script.slice(start);
 assert.ok(start>=0);
 const end=rest.indexOf('\n    function ',5);
 return rest.slice(0,end);
}
const context=vm.createContext({draft:{channels:[{id:'sd',included:true},{id:'a',included:true},{id:'b',included:true}],duplicateGroups:[{channelIds:['a','b','sd'],keepId:'a'}]},Map});
vm.runInContext(declaration('activeDuplicateGroups'),context);
vm.runInContext(declaration('duplicateDefaultKeepID'),context);
assert.equal(context.activeDuplicateGroups()[0].channels.length,3);
assert.equal(context.duplicateDefaultKeepID(context.activeDuplicateGroups()[0]),'a');
context.draft.channels[1].included=false;
assert.equal(context.activeDuplicateGroups()[0].channels.length,2);
assert.equal(context.duplicateDefaultKeepID(context.activeDuplicateGroups()[0]),'b');
context.draft.channels[2].included=false;
assert.equal(context.activeDuplicateGroups().length,0);
assert.ok(script.includes('input.checked = channel.id === defaultKeepID'));
context.els={duplicateReviewList:{querySelectorAll(selector){return selector==='fieldset'?[{querySelector(){return context.keep}}]:[{}]}},duplicateReviewConfirm:{}};
vm.runInContext(declaration('updateDuplicateReviewCount'),context);
context.keep=null;context.updateDuplicateReviewCount();assert.equal(context.els.duplicateReviewConfirm.disabled,true);
context.keep={};context.updateDuplicateReviewCount();assert.equal(context.els.duplicateReviewConfirm.disabled,false);
console.log('Grouped duplicate defaults, counts and keeper guard passed');
