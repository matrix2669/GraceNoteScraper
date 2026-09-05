const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const script=fs.readFileSync('lineuparr.html','utf8').split('<script>')[1].split('</script>')[0];
new vm.Script(script);
const start=script.indexOf('    async function openExport('),end=script.indexOf('\n    els.exportOpen.addEventListener',start);
assert.ok(start>=0 && end>start);
const node=()=>({value:'',hidden:false,disabled:false,textContent:'',classList:{toggle(){}},focus(){},select(){},showModal(){}});
const els=Object.fromEntries(['exportResult','exportURL','exportStatus','exportClose','exportDialog','exportInternalCopy','exportInternalResult','exportInternalURL','exportCopy','exportDownload'].map(k=>[k,node()]));
let publications=0,settings='http://gracenote-dev:8080',copied='',downloadURL='';
const context=vm.createContext({els,URL,Blob,console,draft:{sourceFingerprint:'test'},saving:false,pendingChannelSaves:0,publishing:false,publishedExport:null,internalExportBase:'',exportSettingsRequest:0,
 window:{isSecureContext:true,location:{href:'https://guide.example/lineuparr'}},
 navigator:{clipboard:{async writeText(value){copied=value}}},
 api:async(path)=>{if(path==='/api/setup/share-links'){if(settings===null)throw Error('unavailable');return {internalBaseURL:settings}}publications++;return {path:'/lineuparr/exports/US_Test-11743_lineup.json',filename:'US_Test-11743_lineup.json'}},
 document:{execCommand(){return true},createElement(){return {click(){},remove(){}}},body:{append(){}}},
 fetch:async(url)=>{downloadURL=url;return {ok:true,blob:async()=>new Blob(['{}'])}},setTimeout:()=>{},
});
vm.runInContext(script.slice(start,end),context);
(async()=>{
 await context.openExport();assert.equal(publications,0);assert.equal(els.exportInternalCopy.hidden,false);
 await context.exportLineup('copy-internal');assert.equal(publications,1);assert.equal(copied,'http://gracenote-dev:8080/lineuparr/exports/US_Test-11743_lineup.json');
 await context.exportLineup('copy');assert.equal(publications,1);assert.equal(copied,'https://guide.example/lineuparr/exports/US_Test-11743_lineup.json');
 await context.exportLineup('download');assert.ok(downloadURL.startsWith('https://guide.example/'));assert.equal(publications,1);assert.ok(els.exportStatus.textContent.startsWith('Download started'));
 settings='';await context.openExport();assert.equal(els.exportInternalCopy.hidden,true);await context.exportLineup('copy-internal');assert.equal(publications,1);
 settings=null;await context.openExport();await context.exportLineup('copy');assert.equal(publications,2);assert.equal(els.exportInternalCopy.hidden,true);
 console.log('Internal export: same snapshot, correct origins, optional fallback and no publication on open passed');
})().catch(e=>{console.error(e);process.exitCode=1});
