const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const script=fs.readFileSync('lineuparr.html','utf8').split('<script>')[1].split('</script>')[0];
new vm.Script(script);
const start=script.indexOf('    async function generateLineup('),end=script.indexOf('\n    els.exportOpen.addEventListener',start);
assert.ok(start>=0 && end>start);
let publications=0, summaries=0, downloads=0, message='';
const current={included:'i',aliases:'a',categories:'c',rows:{}};
const context=vm.createContext({
  publishing:false,draft:{sourceFingerprint:'source',exportSignatures:{}},saving:false,pendingChannelSaves:0,
  els:{exportOpen:{disabled:false}},localExportChanges:new Set(['aliases']),
  api:async(path)=>{assert.equal(path,'/api/lineuparr/publish');publications++;return {path:'/lineuparr/exports/US_Test-11743_lineup.json',filename:'US_Test-11743_lineup.json',signatures:current}},
  async refreshExportSummary(){summaries++},showMessage(value){message=value},renderSummary(){},fetch(){downloads++},
});
vm.runInContext(script.slice(start,end),context);
(async()=>{
 await context.generateLineup();
 assert.equal(publications,1);assert.equal(summaries,1);assert.equal(downloads,0);
 assert.deepEqual(context.draft.exportSignatures,current);assert.equal(context.localExportChanges.size,0);
 assert.match(message,/URLs now serve this saved version/);
 console.log('Generate action publishes once, updates signatures, clears stale flags and does not download');
})().catch(e=>{console.error(e);process.exitCode=1});
