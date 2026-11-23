const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const script=fs.readFileSync('lineuparr.html','utf8').split('<script>')[1].split('</script>')[0];
new vm.Script(script);
assert.ok(script.includes("document.querySelector('#source-panel > .panel-body').prepend(tmdbPanel)"));
assert.ok(!script.includes("document.querySelector('main').prepend(tmdbPanel)"));
const nodes=[]; const make=()=>{const n={append(){},prepend(){},setAttribute(){},addEventListener(t,f){this[t]=f}};nodes.push(n);return n};
let state='not-configured', posts=0, timers=0;
const context=vm.createContext({document:{createElement:make,querySelector:make},clearTimeout(){},setTimeout(){timers++;},loadDraft:async()=>{},api:async(path,opts)=>{if(opts){posts++;return {message:'scanned'}}return {state,message:state==='not-configured'?'Add TMDB_TOKEN':state}}});
vm.runInContext(script.slice(script.indexOf('    const tmdbPanel ='),script.indexOf('    initCollapsibleSections();',script.indexOf('    const tmdbPanel ='))),context);
(async()=>{
 await context.loadTMDBCategoryState(); const button=nodes[3];
 assert.equal(button.hidden,true); assert.equal(button.disabled,true);assert.equal(posts,0);
 state='enriching';await context.loadTMDBCategoryState();assert.equal(button.disabled,true);
 state='ready';await context.loadTMDBCategoryState();assert.equal(button.disabled,false);
 await button.click();assert.equal(posts,1);
 state='current';const before=timers;await context.loadTMDBCategoryState();assert.ok(timers>before,'completed scans must keep checking for fresh evidence');
 console.log('TMDB category UI: token note, busy state, manual scan and freshness polling passed');
})().catch(error=>{console.error(error);process.exitCode=1});
