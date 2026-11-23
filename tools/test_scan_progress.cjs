const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const html=fs.readFileSync('lineuparr.html','utf8'), script=html.split('<script>')[1].split('</script>')[0];
new vm.Script(script);
const nodes=new Map();
function node(id){if(!nodes.has(id))nodes.set(id,{style:{},append(){},replaceChildren(){},addEventListener(){}});return nodes.get(id)}
const els=new Proxy({}, {get:(_,id)=>node(id)});
const c=vm.createContext({els,document:{getElementById:node,createElement:()=>node(Symbol())},scanStarting:false,scanRequestError:'',marketPoll:null,marketWasRunning:false,setMarketMessage:()=>{},setInterval:()=>1,clearInterval(){},loadAliasIndex(){},reloadDraft:()=>Promise.resolve(),showMessage(){}});
vm.runInContext(script.slice(script.indexOf('    function renderAliasIndex('),script.indexOf('    async function loadAliasIndex(')),c);
for(const action of ['market','scheduled','guide']){
 c.renderAliasIndex({job:{action,running:true,completedCount:9,totalCount:10,currentProvider:'FOREIGN',completedAt:'2026-09-05T00:00:00Z'}});
 assert.equal(els.scanProgressCount.textContent,'');assert.equal(els.scanProgressBar.style.width,'0%');assert.equal(els.stopScan.hidden,true);assert.equal(els.postalScan.disabled,true);
 assert.equal(node('alias-last-refresh').textContent,'Last provider refresh: never');assert.ok(!els.scanStatus.textContent.includes('FOREIGN'));
}
c.renderAliasIndex({job:{action:'postal',running:true,completedCount:2,totalCount:8,currentProvider:'Local provider'}});
assert.equal(els.scanProgressBar.style.width,'25%');assert.equal(els.stopScan.hidden,false);
c.renderAliasIndex({job:{action:'market',running:false,lastError:'foreign error'},postalScan:{status:'complete',completedAt:'2026-09-05T00:00:00Z'}});
assert.equal(els.scanProgressBar.style.width,'100%');assert.ok(!node('alias-last-refresh').textContent.includes('never'));
const view={job:{action:'postal',running:true,completedCount:7,totalCount:8,currentProvider:'LOCAL'},next:{rank:1,name:'New York',postalCode:'10001'},catalog:{asOf:'2025-09'},scans:[]};
Object.assign(c,{api:async()=>view,clearTimeout(){},setTimeout:()=>1});
vm.runInContext(script.slice(script.indexOf('    const marketStart =')),c);
(async()=>{
 await c.loadMajorMarkets();assert.equal(node('major-market-progress-count').textContent,'');assert.equal(node('major-market-progress-bar').style.width,'0%');
 view.job={action:'market',running:true,completedCount:3,totalCount:6,currentProvider:'Verizon'};
 await c.loadMajorMarkets();assert.equal(node('major-market-progress-bar').style.width,'50%');assert.match(node('major-market-status').textContent,/Verizon/);
 view.job={action:'postal',running:false,lastError:'local failure'};await c.loadMajorMarkets();assert.ok(!node('major-market-status').textContent.includes('local failure'));
 console.log('Scan ownership: local, market, scheduled, idle, refresh timestamp and shared busy lock passed');
 if(process.env.SCAN_LAYOUT){
  const {chromium}=require('playwright');const browser=await chromium.launch({headless:true,channel:'chrome'});
  try{const page=await browser.newPage();await page.route('**/*',r=>r.abort());await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
   await page.locator('#major-market-panel').evaluate(n=>n.open=true);
   for(const width of [320,390,720,1080,1440]){await page.setViewportSize({width,height:1000});assert.equal(await page.locator('#major-market-panel').evaluate(n=>n.scrollWidth>n.clientWidth),false,`overflow at ${width}`)}
   assert.equal(await page.locator('#major-market-panel > .panel-summary').count(),1);assert.equal(await page.locator('#major-market-panel .scan-progress .progress-bar').count(),1);
   console.log('Major-market matching panel structure and five responsive widths passed');
  }finally{await browser.close()}
 }
})().catch(e=>{console.error(e);process.exitCode=1});
