const {chromium}=require('playwright');
const fs=require('node:fs'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
 try {
  const page=await browser.newPage();await page.route('**/*',r=>r.abort());
  const html=fs.readFileSync('lineuparr.html','utf8');
  const script=html.split('<script>')[1].split('</script>')[0];
  assert.ok(!script.includes("confirm.textContent = 'Confirm category'"));
  await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
  const code=script.slice(script.indexOf('    const categoryReviewSelection'),script.indexOf('    function renderCategoryReviewReport()'));
  await page.evaluate(code=>{
   const panel=document.getElementById('category-review-panel');document.querySelector('main').replaceChildren(panel);
   window.draft={sourceFingerprint:'fixture',channels:[{id:'a',number:'53',callSign:'FREEFRM',name:'Freeform',category:'Entertainment',categorySource:'provider',categoryPriority:4,categoryMethod:'Long provenance '.repeat(50),included:true,needsCategoryReview:true},{id:'b',number:'104',callSign:'CSPAN2',name:'C-SPAN 2',category:'News & Weather',categorySource:'provider',categoryPriority:3,included:true,needsCategoryReview:true},{id:'c',included:false,needsCategoryReview:true},{id:'d',number:'7',callSign:'HBOHTS',name:'HBO Hits',category:'Movies',categorySource:'tmdb',categoryPriority:3,included:true,needsCategoryReview:true}]};
   window.categories=()=>['Entertainment','News & Weather','Movies'];window.sourceLabel=()=> 'Official provider';
   window.saves=[];window.saveChannel=async(channel,patch)=>{saves.push(patch);channel.category=patch.category;channel.needsCategoryReview=false;};
   window.programs=[];window.lineuparrOpenChannelPrograms=channel=>programs.push(channel.id);
   (0,eval)(code+'\nwindow.renderCategoryReview = renderCategoryReview;');renderCategoryReview();
  },code);
  assert.equal(await page.locator('.category-review-row').count(),3);
  assert.match(await page.locator('#category-review-count').textContent(),/3 remaining/);
  assert.deepEqual(await page.locator('.category-review-group-title').allTextContents(),['Priority 3 (2)','Priority 4 (1)']);
  assert.match(await page.locator('.category-review-channel-link').filter({hasText:'FREEFRM'}).textContent(),/FREEFRM.*Freeform/);
  await page.locator('.category-review-channel-link').filter({hasText:'FREEFRM'}).click();
  assert.deepEqual(await page.evaluate(()=>programs),['a']);
  for(const width of [320,390,720,1080,1440]) {
   await page.setViewportSize({width,height:900});
   const result=await page.evaluate(()=>({overflow:document.documentElement.scrollWidth>innerWidth,selects:[...document.querySelectorAll('.category-review-controls select')].map(x=>x.getBoundingClientRect().width)}));
   assert.equal(result.overflow,false,`overflow at ${width}`);assert.ok(result.selects.every(x=>x>=180),`squeezed selector at ${width}`);
   const fonts=await page.locator('.category-review-row').first().evaluate(n=>({name:getComputedStyle(n.querySelector('.channel-name')).fontWeight,meta:getComputedStyle(n.querySelector('.channel-meta')).fontSize,select:getComputedStyle(n.querySelector('select')).fontSize,button:getComputedStyle(n.querySelector('.category-review-controls button')).fontSize}));
   assert.deepEqual(fonts,{name:'760',meta:'11px',select:'13px',button:'13px'});
   const checks=await page.locator('#category-review-panel input[type="checkbox"]').evaluateAll(nodes=>nodes.map(n=>{const a=n.getBoundingClientRect(),b=n.parentElement.getBoundingClientRect();return {w:a.width,h:a.height,aligned:Math.abs(a.y+a.height/2-b.y-b.height/2)<2}}));
   assert.ok(checks.every(n=>n.w===18&&n.h===18&&n.aligned),`oversized or displaced checkboxes at ${width}`);
   if(width>=1080)assert.ok(await page.locator('.category-review-row').first().evaluate(n=>n.getBoundingClientRect().height<150),'collapsed review row should be compact');
  }
  await page.getByLabel('Sort category reviews').selectOption('name');
  assert.match(await page.locator('.category-review-row').first().textContent(),/CSPAN2/);
  await page.getByLabel('Sort category reviews').selectOption('category');
  assert.match(await page.locator('.category-review-row').last().textContent(),/FREEFRM/);
  await page.getByLabel('Filter category reviews by category').selectOption('Entertainment');
  assert.equal(await page.locator('.category-review-row').count(),1);
  assert.match(await page.locator('.category-review-visible').textContent(),/Showing 1 of 3/);
  await page.locator('.category-review-controls select').first().selectOption('Movies');
  await page.getByRole('button',{name:'Save correction',exact:true}).click();
  assert.equal(await page.locator('.category-review-row').count(),0);
  assert.deepEqual(await page.evaluate(()=>saves),[{category:'Movies'}]);
  assert.match(await page.locator('#category-review-list').textContent(),/No pending category reviews match this filter/);
  await page.getByLabel('Filter category reviews by category').selectOption('');
  await page.getByRole('button',{name:'Confirm',exact:true}).first().click();
  await page.evaluate(()=>{
   for (const channel of draft.channels) if (channel.id !== 'c') channel.needsCategoryReview=true;
   window.api=async(path,request)=>{window.batchRequest=JSON.parse(request.body);for(const row of batchRequest.channels)draft.channels.find(c=>c.id===row.id).needsCategoryReview=false;return {saved:true}};
   window.reloadDraft=async()=>{};window.showMessage=()=>{};renderCategoryReview();
  });
  await page.getByLabel('Filter category reviews by category').selectOption('Movies');
  await page.getByLabel('Select all shown').check();
  await page.getByRole('button',{name:'Approve selected (2)',exact:true}).click();
  assert.deepEqual(await page.evaluate(()=>batchRequest.channels),[{id:'a',category:'Movies'},{id:'d',category:'Movies'}]);
  await page.getByLabel('Filter category reviews by category').selectOption('');
  await page.getByLabel('Select all shown').check();
  await page.getByRole('button',{name:'Approve selected (1)',exact:true}).click();
  assert.match(await page.locator('#category-review-list').textContent(),/No included channels/);
  console.log('Category review: priority groups, name links, filters, sorting, responsive layout and batch approval passed');
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
