const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const html=fs.readFileSync('lineuparr.html','utf8');const script=html.split('<script>')[1].split('</script>')[0];
const nodes=new Map(),created=[],requests=[];
function make(){return {style:{},append(){},replaceChildren(){},addEventListener(type,fn){this[type]=fn}}}
function node(id){if(!nodes.has(id))nodes.set(id,make());return nodes.get(id)}
const scan={marketRank:1,postalCode:'10001',status:'complete',providerAudit:[],allProviderYield:{aliases:1,categories:1,currentStations:1},newFamilyYield:{aliases:1,categories:1,currentStations:1}};
const view={job:{running:false},next:{rank:2,name:'Los Angeles',postalCode:'90012'},catalog:{asOf:'2025-09'},scans:[scan]};
const context=vm.createContext({document:{getElementById:node,createElement:()=>{const n=make();created.push(n);return n}},api:async(path,options={})=>{requests.push({path,options});return view},loadAliasIndex:async()=>{},clearTimeout(){},setTimeout:()=>1});
vm.runInContext(script.slice(script.indexOf('    const marketStart =')),context);
(async()=>{
 await context.loadMajorMarkets();const button=created.find(n=>n.textContent==='Rescan ZIP 10001');assert.ok(button);assert.equal(button.disabled,false);
 await button.click();const post=requests.find(r=>r.options.method==='POST');assert.equal(post.path,'/api/lineuparr/markets');assert.deepEqual(JSON.parse(post.options.body),{rank:1});assert.equal(view.next.rank,2);
 created.length=0;view.job={running:true,action:'postal'};await context.loadMajorMarkets();assert.equal(created.find(n=>n.textContent==='Rescan ZIP 10001').disabled,true);
 console.log('Market rescan: explicit rank POST, saved-report control and shared busy state passed');
})().catch(e=>{console.error(e);process.exitCode=1});
