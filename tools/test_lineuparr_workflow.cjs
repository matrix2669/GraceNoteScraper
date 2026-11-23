const {chromium}=require('playwright');
const fs=require('node:fs'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
 try {
  const page=await browser.newPage();await page.route('**/*',r=>r.abort());
  const html=fs.readFileSync('lineuparr.html','utf8');
  const start=html.indexOf('    function arrangeLineuparrWorkflow()');
  const end=html.indexOf("    document.addEventListener('DOMContentLoaded'",start);
  assert.ok(start>0&&end>start);
  await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
  await page.evaluate(code=>{
   window.els={exportOpen:document.querySelector('#export-open')||document.createElement('button')};
   const main=document.querySelector('main');
   for(const id of ['source-panel','alias-panel','major-market-panel','tmdb-category-panel','category-review-panel','dispatch-panel']){
    if(!document.getElementById(id)){const d=document.createElement('details');d.id=id;main.append(d)}
   }
   window.calls=[];window.api=async(path)=>{calls.push(path);return path.endsWith('share-links')?{internalBaseURL:'http://gracenotescraper:8080'}:{exists:true,filename:'US_Test-10001_lineup.json',publishedAt:'2026-09-05T12:00:00Z',path:'/lineuparr/exports/US_Test-10001_lineup.json'}};
   window.copyExportURL=async(input)=>{window.copied=input.value;return true};
   // about:blank has no URL origin; use a stable fixture base without navigating.
   code=code.replace('window.location.href',"'http://localhost:8080/lineuparr'");
   (0,eval)(code+'\nwindow.refreshExportSummary=refreshExportSummary;arrangeLineuparrWorkflow();');
  },html.slice(start,end));
  await page.evaluate(()=>refreshExportSummary());
  const order=await page.locator('main > *').evaluateAll(nodes=>nodes.slice(-8).map(n=>n.id||'channels'));
  assert.deepEqual(order,['source-panel','alias-panel','major-market-panel','tmdb-category-panel','category-review-panel','channels','dispatch-panel','export-panel']);
  assert.match(await page.locator('#published-export-summary').textContent(),/Created:/);
  assert.equal(await page.getByLabel('Docker-network lineup URL',{exact:true}).inputValue(),'http://gracenotescraper:8080/lineuparr/exports/US_Test-10001_lineup.json');
  assert.match(await page.getByRole('link',{name:'Download saved JSON'}).getAttribute('href'),/\?download=1$/);
  await page.getByLabel('Lineup URL',{exact:true}).click();
  assert.match(await page.evaluate(()=>copied),/US_Test-10001_lineup.json$/);
  await page.evaluate(()=>refreshExportSummary());
  assert.equal(await page.getByRole('link',{name:'Download saved JSON'}).count(),1);
  assert.ok((await page.evaluate(()=>calls)).every(p=>!p.includes('publish')));
  for(const width of [320,390,720,1080,1440]){
   await page.setViewportSize({width,height:900});
   const overflow=await page.locator('#export-panel').evaluate(n=>n.scrollWidth>n.clientWidth);
   assert.equal(overflow,false,'Export overflow at '+width);
  }
  console.log('Workflow order, saved summary, copy/download, no republish and five export widths passed');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
