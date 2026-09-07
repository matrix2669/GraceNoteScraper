const {chromium}=require('playwright');
const fs=require('node:fs'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
 try {
  const page=await browser.newPage();await page.route('**/*',r=>r.abort());
  const html=fs.readFileSync('lineuparr.html','utf8');
  const start=html.indexOf('    function setWorkflowHeading(');
  const end=html.indexOf("    document.addEventListener('DOMContentLoaded'",start);
  assert.ok(start>0&&end>start);
  await page.setContent(html.replace('<head>','<head><base href="http://localhost/lineuparr">').replace(/<script>[\s\S]*?<\/script>/g,''));
  await page.evaluate(({code})=>{
   const main=document.querySelector('main');
   const add=(id)=>{const panel=document.createElement('details');panel.id=id;panel.className='panel';panel.append(document.createElement('summary'));const body=document.createElement('div');body.className='panel-body';panel.append(body);main.append(panel)};
   for(const id of ['alias-panel','major-market-panel','tmdb-category-panel','category-review-panel','dispatch-panel'])add(id);
   window.els={exportOpen:document.getElementById('export-open')};
   window.draft={sourceFingerprint:'source',customizationSignature:'custom',exportSignatures:{included:'i',aliases:'a',categories:'c',rows:{}},channels:[{id:'one',included:true,category:'News',needsCategoryReview:true}]};
   window.saving=false;window.localExportChanges=new Set();window.workflowProgress={};window.workflowRemote={};window.workflowVector='';window.workflowInitialized=false;window.workflowRefreshTimer=0;window.workflowRefreshing=false;window.savedExport=null;window.savedExportDownload=null;window.exportSummaryRequest=0;
   window.responses={
    '/api/lineuparr/workflow':{},
    '/api/lineuparr/alias-index':{postalScan:{status:'complete'}},
    '/api/lineuparr/markets':{scans:[{status:'complete',providerAudit:[{access:'error'}]}]},
    '/api/lineuparr/tmdb-categories':{state:'not-configured',message:'No token'},
    '/api/lineuparr/dispatcharr/config':{configured:false},
    '/api/lineuparr/export-summary':{exists:true,filename:'US_Test-10001_lineup.json',publishedAt:'2026-09-05T12:00:00Z',path:'/lineuparr/exports/US_Test-10001_lineup.json',signatures:{included:'i',aliases:'a',categories:'c',rows:{}}},
    '/api/setup/share-links':{internalBaseURL:'http://gracenotescraper:8080'}
   };
   window.api=async(path,options={})=>{
    if(path==='/api/lineuparr/workflow'&&options.method==='POST'){
      const action=JSON.parse(options.body).action;
      if(action==='skip-tmdb')responses[path]={tmdbDisposition:'skipped'};
      if(action==='complete-customization')responses[path]={tmdbDisposition:'skipped',customizationSignature:'custom'};
    }
    return structuredClone(responses[path]);
   };
   window.showMessage=()=>{};
   (0,eval)(code+'\nwindow.arrangeLineuparrWorkflow=arrangeLineuparrWorkflow;window.loadWorkflowStatus=loadWorkflowStatus;window.renderGuidedWorkflow=renderGuidedWorkflow;window.refreshExportSummary=refreshExportSummary;');
   arrangeLineuparrWorkflow();
  },{code:html.slice(start,end)});
  await page.evaluate(()=>loadWorkflowStatus());
  const order=await page.locator('main > *').evaluateAll(nodes=>nodes.slice(-8).map(n=>n.id||'channels'));
  assert.deepEqual(order,['source-panel','alias-panel','major-market-panel','tmdb-category-panel','category-review-panel','channels','dispatch-panel','export-panel']);
  assert.equal(await page.locator('#alias-panel .step-state').textContent(),'Complete');
  assert.equal(await page.locator('#major-market-panel .step-state').textContent(),'Incomplete');
  assert.equal(await page.locator('#major-market-panel').getAttribute('open'),'');
  assert.equal(await page.locator('#alias-panel').getAttribute('open'),null);

  await page.evaluate(async()=>{responses['/api/lineuparr/markets'].scans[0].providerAudit=[{access:'enriched'},{access:'error'}];await loadWorkflowStatus()});
  assert.equal(await page.locator('#major-market-panel .step-state').textContent(),'Complete');
  assert.equal(await page.locator('#tmdb-category-panel').getAttribute('open'),'');
  await page.getByRole('button',{name:'Skip TMDB Category Enrichment'}).click();
  assert.equal(await page.locator('#tmdb-category-panel .step-state').textContent(),'Skipped');
  assert.equal(await page.locator('#category-review-panel').getAttribute('open'),'');

  await page.evaluate(()=>{draft.channels[0].needsCategoryReview=false;renderGuidedWorkflow()});
  assert.equal(await page.locator('#category-review-panel .step-state').textContent(),'Complete');
  assert.equal(await page.locator('.builder-panel').getAttribute('open'),'');
  await page.evaluate(()=>completeCustomizationWorkflow());
  assert.equal(await page.locator('.builder-panel .step-state').textContent(),'Complete');
  assert.equal(await page.locator('#dispatch-panel').getAttribute('open'),'');
  assert.equal(await page.locator('#tmdb-workflow-skip').count(),1);

  await page.evaluate(()=>refreshExportSummary());
  assert.equal(await page.getByRole('button',{name:'Re-generate Lineup File'}).count(),1);
  assert.equal(await page.locator('#saved-export-download').getAttribute('href'),'http://localhost/lineuparr/exports/US_Test-10001_lineup.json?download=1');
  assert.equal(await page.locator('#saved-export-download').isHidden(),false);
  assert.match(await page.locator('#published-export-summary').textContent(),/Docker-network lineup URL/);
  assert.equal(await page.locator('#export-panel').evaluate(n=>n.tagName),'SECTION');
  assert.equal(await page.locator('#export-panel summary').count(),0);

  await page.evaluate(async()=>{localExportChanges.add('included');localExportChanges.add('aliases');localExportChanges.add('categories');await refreshExportSummary()});
  const stale=await page.locator('.export-stale').textContent();
  assert.match(stale,/Included channels have changed/);assert.match(stale,/Channel aliases have changed/);assert.match(stale,/Channel categories have changed/);
  for(const width of [320,390,720,1080,1440]){await page.setViewportSize({width,height:900});assert.equal(await page.locator('#export-panel').evaluate(n=>n.scrollWidth>n.clientWidth),false,'Export overflow at '+width)}
  console.log('Guided step completion, advisory accordion, TMDB skip, permanent export panel and stale notices passed');
 }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
