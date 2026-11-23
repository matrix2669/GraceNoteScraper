const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const path = require('node:path');
const html = fs.readFileSync(path.join(__dirname, '../lineuparr.html'), 'utf8');
for (const script of html.matchAll(/<script[^>]*>([\s\S]*?)<\/script>/g)) new vm.Script(script[1]);
class Element {
  constructor(){ this.children=[];this.dataset={};this.listeners={};this.checked=false;this.hidden=false; }
  replaceChildren(...children){this.children=children;}
  append(...children){this.children.push(...children);}
  addEventListener(name, callback){this.listeners[name]=callback;}
  querySelectorAll(){return this.children.flatMap(child=>[...child.children,...child.querySelectorAll()]).filter(child=>child.type==='checkbox');}
}
const nodes = new Map();
const element = id => {if(!nodes.has(id)) nodes.set(id,new Element());return nodes.get(id);};
let calls=[],loads=[],messages=[],approve=true,response;
const preview={token:'current',manualCount:2,rows:[
 {key:'a',group:'group',channelNumber:'502',channelName:'WCBSDT',streamName:'US: CBS (KTVT)',accountId:3,reason:'KTVT differs from WCBS'},
 {key:'b',group:'group',channelNumber:'502',channelName:'WCBSDT',streamName:'US: CBS (KTVT) HD',accountId:4,reason:'KTVT differs from WCBS'},
]};
const context=vm.createContext({
 document:{getElementById:element,createElement(){return new Element();},querySelectorAll(){return element('affiliate-rows').querySelectorAll();}},
 dispatchBusy:false,dispatchReviewEpoch:0,decisionRevision:0,
 window:{confirm(){return approve;}},
 setDispatchControls(){},showMessage(...args){messages.push(args);},
 async api(url,options){calls.push([url,options]);if(response)return response(url,options);return options?{saved:2}:preview;},
 async loadMatchReview(force,interactive){loads.push([force,interactive]);},async loadDraft(){loads.push('draft');},
});
const start=html.indexOf('    let affiliatePreview = null;');
const end=html.indexOf('    function renderMatchReview()',start);
vm.runInContext(html.slice(start,end),context);
async function test(){
 await vm.runInContext('previewAffiliateExclusions()',context);
 let inputs=element('affiliate-rows').querySelectorAll();
 assert.equal(inputs.length,2);assert(inputs.every(x=>!x.checked));
 assert.equal(calls.filter(x=>x[1]).length,0,'preview wrote decisions');
 inputs[0].checked=true;inputs[0].listeners.change();assert(inputs.every(x=>x.checked),'group variants not selected together');
 approve=false;await vm.runInContext('applyAffiliateExclusions()',context);assert.equal(calls.filter(x=>x[1]).length,0);
 approve=true;await vm.runInContext('applyAffiliateExclusions()',context);
 assert.equal(calls.find(x=>x[1])[1].headers['Content-Type'],'application/json');
 const body=JSON.parse(calls.find(x=>x[1])[1].body);assert.deepEqual(body,{token:'current',keys:['a','b']});
 assert.deepEqual(loads,[[false,false],'draft'],'successful apply did not refresh while busy');
 assert.equal(context.dispatchBusy,false);assert(element('affiliate-review').hidden);
 // A stale apply must clear the preview, preserve visible review and report it.
 await vm.runInContext('previewAffiliateExclusions()',context);
 inputs=element('affiliate-rows').querySelectorAll();inputs[0].checked=true;inputs[0].listeners.change();
 const previousLoads=loads.length;response=async()=>{throw new Error('Review changed; preview again');};
 await vm.runInContext('applyAffiliateExclusions()',context);
 assert.equal(loads.length,previousLoads);assert(element('affiliate-review').hidden);assert(messages.at(-1)[1]);
 // An obsolete preview response must not install a new preview after epoch change.
 response=async()=>{context.dispatchReviewEpoch++;return preview;};
 await vm.runInContext('previewAffiliateExclusions()',context);
 assert(element('affiliate-review').hidden);
 console.log('Affiliate UI: side-effect-free preview, raw-variant group selection, confirmation, noninteractive reload, stale/late fencing passed');
}
test().catch(error=>{console.error(error);process.exitCode=1;});
