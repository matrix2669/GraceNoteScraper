const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const script=fs.readFileSync('lineuparr.html','utf8').split('<script>')[1].split('</script>')[0];new vm.Script(script);
const nodes=new Map(),requests=[];
function node(id){if(!nodes.has(id))nodes.set(id,{style:{},append(){},replaceChildren(){},addEventListener(type,cb){this[type]=cb}});return nodes.get(id)}
const view={job:{running:false},next:{rank:1,name:'New York',postalCode:'10001'},catalog:{asOf:'2025-09'},scans:[]};
const c=vm.createContext({document:{getElementById:node,createElement:()=>node(Symbol())},api:async(path,options={})=>{requests.push({path,options});return view},clearTimeout(){},setTimeout(){throw Error('idle polling')},loadAliasIndex:async()=>{}});
vm.runInContext(script.slice(script.indexOf('    const marketStart =')),c);
(async()=>{await c.loadMajorMarkets();assert.equal(requests.some(r=>r.options.method==='POST'),false);await node('major-market-start').click();const post=requests.find(r=>r.options.method==='POST');assert.equal(post.path,'/api/lineuparr/markets');assert.equal(post.options.headers['Content-Type'],'application/json');assert.equal(post.options.body,'{}');assert.ok(node('major-market-next').textContent.includes('10001'));console.log('Market UI: no automatic scan, one explicit address-free POST and generic next-market status passed')})().catch(e=>{console.error(e);process.exitCode=1});
