// Automatic identity UI supersedes the retired bulk selection controls.
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict'),path=require('node:path');
const html=fs.readFileSync(path.join(__dirname,'../lineuparr.html'),'utf8');
for(const script of html.matchAll(/<script[^>]*>([\s\S]*?)<\/script>/g))new vm.Script(script[1]);
class Element{constructor(){this.children=[];this.textContent='';}replaceChildren(){this.children=[];}append(item){this.children.push(item);}}
const nodes=new Map(),element=id=>{if(!nodes.has(id))nodes.set(id,new Element());return nodes.get(id);};
const context=vm.createContext({document:{getElementById:element,createElement:()=>new Element()},emptyMessage:text=>Object.assign(new Element(),{textContent:text})});
const start=html.indexOf('    function renderAutomaticExclusions('),end=html.indexOf('    function renderMatchReview()',start);
assert(start>=0&&end>start);vm.runInContext(html.slice(start,end),context);
context.view={message:'Current snapshot',rows:[
 {channelNumber:'502',channelName:'WCBSDT',streamName:'US: CBS (KTVT)',accountId:3,exported:true,reason:'Conflicting callsign'},
 {channelNumber:'901',channelName:'HBOHDP',streamName:'US: HBO HD',accountId:7,exported:true,pendingReview:true,reason:'Unknown feed'},
 {channelNumber:'901',channelName:'HBOHDP',streamName:'US: HBO East',accountId:3,conflict:true,exported:false,reason:'Opposing feed'},
 {manualDecision:'denied',exported:true,pendingReview:true,reason:'Saved denial'},
]};
vm.runInContext('renderAutomaticExclusions(view)',context);
assert.equal(element('automatic-count').textContent,'(3 exclusions, 1 manual, 1 confirmation conflicts)');
assert.match(element('automatic-rows').children[0].textContent,/Automatically excluded/);
assert.match(element('automatic-rows').children[1].textContent,/Manual review required/);
assert.match(element('automatic-rows').children[2].textContent,/Saved confirmation preserved/);
assert.match(element('automatic-rows').children[3].textContent,/Saved denial preserved/);
context.view.rows=Array.from({length:201},()=>context.view.rows[0]);vm.runInContext('renderAutomaticExclusions(view)',context);
assert.equal(element('automatic-rows').children.length,201);assert.match(element('automatic-rows').children.at(-1).textContent,/Showing 200 of 201/);
vm.runInContext('renderAutomaticExclusions(null)',context);assert.equal(element('automatic-rows').children.length,0);assert.match(element('automatic-message').textContent,/Refresh streams/);
assert(!html.includes('id="affiliate-apply"'));assert(!html.includes('id="affiliate-preview"'));
console.log('Automatic UI: distinct automatic/pending/confirmation/denial states, full-name text safety, count cap and stale clearing passed');
