const {chromium}=require('playwright');
const fs=require('node:fs'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
 try {
  const page=await browser.newPage();await page.route('**/*',r=>r.abort());
  const html=fs.readFileSync('lineuparr.html','utf8');
  const script=html.split('<script>')[1].split('</script>')[0];
  assert.ok(!script.includes("confirm.textContent = 'Confirm category'"));
  assert.ok(!script.includes("Save correction"));
  assert.ok(!script.includes("Approve selected"));
  await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
  const code=script.slice(script.indexOf('    const categoryReviewSelection'),script.indexOf('    function renderCategoryReviewReport()'))
   + script.slice(script.indexOf('    function equivalentChannelNames('),script.indexOf('    function filteredChannels()'))
   + script.slice(script.indexOf('    function channelCard('),script.indexOf('    async function setAlias('))
   + script.slice(script.indexOf('    function badge('),script.indexOf('    function toggleBatchMode('));
  await page.evaluate(code=>{
   const panel=document.getElementById('category-review-panel');document.querySelector('main').replaceChildren(panel);panel.open=true;
   window.draft={sourceFingerprint:'fixture',sources:[],channels:[{id:'a',number:'53',callSign:'FREEFRM',name:'Freeform',category:'Entertainment',categorySource:'provider',categoryPriority:4,categoryMethod:'Long provenance '.repeat(50),included:true,needsCategoryReview:true},{id:'b',number:'104',callSign:'CSPAN2',name:'C-SPAN 2',category:'News & Weather',categorySource:'provider',categoryPriority:3,included:true,needsCategoryReview:true},{id:'c',included:false,needsCategoryReview:true},{id:'d',number:'7',callSign:'HBOHTS',name:'HBO Hits',category:'Movies',categorySource:'tmdb',categoryPriority:3,included:true,needsCategoryReview:true}]};
   window.noteLocalExportEdit=()=>{};window.selectedChannels=new Set();window.saving=false;
   window.categories=()=>['Entertainment','News & Weather','Movies'];window.sourceLabel=()=> 'Official provider';
   window.saves=[];window.saveChannel=async(channel,patch)=>{saves.push({id:channel.id,...patch});channel.category=patch.category;channel.needsCategoryReview=false;return true;};
   window.programs=[];window.lineuparrOpenChannelPrograms=channel=>programs.push(channel.id);
   (0,eval)(code+'\nwindow.renderCategoryReview = renderCategoryReview;window.renderLineupCard=channelCard;window.getCategorySelection=()=>[...categoryReviewSelection];window.getCategoryEdits=()=>[...categoryReviewEdits];');renderCategoryReview();
  },code);
  assert.equal(await page.locator('.category-review-row').count(),3);
  assert.match(await page.locator('#category-review-count').textContent(),/3 remaining/);
  assert.deepEqual(await page.locator('.category-review-group-title').allTextContents(),['Priority 3 (2)','Priority 4 (1)']);
  assert.match(await page.locator('.category-review-channel-link').filter({hasText:'FREEFRM'}).textContent(),/53 · Freeform \(FREEFRM\)/);
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
  assert.match(await page.locator('.category-review-row').first().textContent(),/C-SPAN 2/);
  await page.getByLabel('Sort category reviews').selectOption('category');
  assert.match(await page.locator('.category-review-row').last().textContent(),/FREEFRM/);
  await page.getByLabel('Filter category reviews by category').selectOption('Entertainment');
  assert.equal(await page.locator('.category-review-row').count(),1);
  assert.match(await page.locator('.category-review-visible').textContent(),/Showing 1 of 3/);
  await page.getByLabel('Category for 53 Freeform (FREEFRM)').selectOption('Movies');
  assert.equal(await page.getByLabel('Select 53 Freeform (FREEFRM) for confirmation').isChecked(),true);
  assert.equal(await page.getByRole('button',{name:'Commit',exact:true}).count(),1);
  assert.equal(await page.getByRole('button',{name:'Confirm selected (1)',exact:true}).count(),1);
  await page.getByLabel('Sort category reviews').selectOption('number');
  assert.equal(await page.getByLabel('Category for 53 Freeform (FREEFRM)').inputValue(),'Movies');
  await page.getByLabel('Filter category reviews by category').selectOption('');
  assert.equal(await page.getByLabel('Category for 53 Freeform (FREEFRM)').inputValue(),'Movies');
  assert.equal(await page.getByLabel('Select 53 Freeform (FREEFRM) for confirmation').isChecked(),false,'filter changes still clear hidden selection');
  await page.getByLabel('Category for 104 C-SPAN 2').selectOption('Entertainment');
  await page.getByLabel('Select 53 Freeform (FREEFRM) for confirmation').check();
  assert.equal(await page.getByRole('button',{name:'Confirm selected (2)',exact:true}).count(),1);
  await page.locator('.category-review-row').filter({hasText:'FREEFRM'}).getByRole('button',{name:'Commit',exact:true}).click();
  assert.deepEqual(await page.evaluate(()=>saves),[{id:'a',category:'Movies'}]);
  assert.equal(await page.locator('.category-review-row').count(),2);
  assert.equal(await page.getByLabel('Category for 104 C-SPAN 2').inputValue(),'Entertainment','committing one row must retain another pending correction');
  assert.equal(await page.getByLabel('Select 104 C-SPAN 2 for confirmation').isChecked(),true);
  assert.equal(await page.getByRole('button',{name:'Confirm selected (1)',exact:true}).count(),1);
  await page.evaluate(()=>{
   window.batchAttempts=0;window.messages=[];
   window.api=async(path,request)=>{window.batchAttempts++;window.batchRequest=JSON.parse(request.body);if(window.batchAttempts===1)throw new Error('retry batch');for(const row of batchRequest.channels)draft.channels.find(c=>c.id===row.id).needsCategoryReview=false;return {saved:true}};
   window.reloadDraft=async()=>{};window.showMessage=(message,error)=>messages.push({message,error});renderCategoryReview();
  });
  await page.getByLabel('Select 7 HBO Hits (HBOHTS) for confirmation').check();
  await page.getByRole('button',{name:'Confirm selected (2)',exact:true}).click();
  assert.deepEqual(await page.evaluate(()=>batchRequest.channels),[{id:'b',category:'Entertainment'},{id:'d',category:'Movies'}]);
  assert.equal(await page.getByLabel('Category for 104 C-SPAN 2').inputValue(),'Entertainment','failed batch must retain corrections');
  assert.equal(await page.getByLabel('Select 104 C-SPAN 2 for confirmation').isChecked(),true,'failed batch must retain selection');
  assert.equal(await page.getByLabel('Select 7 HBO Hits (HBOHTS) for confirmation').isChecked(),true,'failed batch must retain all selected rows');
  await page.getByRole('button',{name:'Confirm selected (2)',exact:true}).click();
  assert.deepEqual(await page.evaluate(()=>batchRequest.channels),[{id:'b',category:'Entertainment'},{id:'d',category:'Movies'}]);
  assert.match(await page.locator('#category-review-list').textContent(),/No included channels need category review/);
  const identityRows=[
   {id:'west',number:'905',stationId:'station-west',callSign:'HBODHDP',name:'unused',nameSource:'dispatcharr',category:'Movies',included:true,needsCategoryReview:true,categoryPriority:4,serviceEvidence:[{rootStationId:'station-west',serviceFamily:'HBO Drama',sourceId:'registry',sourceRowId:'west-row',sourceRevision:'v1',value:'HBO Drama',feed:'west'}],aliasEvidence:[{value:'HBO Drama',sources:['registry'],methods:['verified exact station']}]},
   {id:'east',number:'904',stationId:'station-east',callSign:'HBODRHD',name:'unused',nameSource:'dispatcharr',category:'Movies',included:true,needsCategoryReview:true,categoryPriority:4,serviceEvidence:[{rootStationId:'station-east',serviceFamily:'HBO Drama',sourceId:'registry',sourceRowId:'east-row',sourceRevision:'v1',value:'HBO Drama',feed:'east'}],aliasEvidence:[{value:'HBO Drama',sources:['registry'],methods:['verified exact station']}]},
   {id:'pacific',number:'801',stationId:'station-pac',callSign:'BBCAM',name:'unused',nameSource:'dispatcharr',category:'Entertainment',included:true,needsCategoryReview:true,categoryPriority:4,serviceEvidence:[{rootStationId:'station-pac',serviceFamily:'BBC America',sourceId:'registry',sourceRowId:'pac-row',sourceRevision:'v1',value:'BBC America',feed:'pacific'}],aliasEvidence:[{value:'BBC America',sources:['registry'],methods:['verified exact station']}]},
   {id:'fallback',number:'51',callSign:'KFallback',name:'KFallback',nameSource:'dispatcharr',category:'Entertainment',included:true,needsCategoryReview:true,categoryPriority:4},
   {id:'unsafe',number:'52',callSign:'SAFE',name:'<img src=x onerror=alert(1)>',nameSource:'provider',category:'Entertainment',included:true,needsCategoryReview:true,categoryPriority:4},
   {id:'id-fallback',number:'777',callSign:'',name:'777',nameSource:'dispatcharr',category:'Entertainment',included:true,needsCategoryReview:true,categoryPriority:4},
   {id:'historical',number:'901',stationId:'hist',callSign:'HBOHTHD',name:'HBOHTHD',nameSource:'dispatcharr',category:'Movies',included:true,needsCategoryReview:true,categoryPriority:4,affiliate:'HBO',aliasEvidence:[{value:'HBO Signature Historical Long Alias',sources:['provider'],methods:['historical provider alias']}]},
   {id:'supplemental',number:'902',stationId:'supp',callSign:'CALL',name:'CALL',nameSource:'dispatcharr',category:'Movies',included:true,needsCategoryReview:true,categoryPriority:4,supplementalEvidence:[{rootStationId:'supp',value:'Verified Full Name'}]},
   {id:'exact-description',number:'903',stationId:'exact',callSign:'EXACTID',name:'EXACTID',nameSource:'dispatcharr',category:'Movies',included:true,needsCategoryReview:true,categoryPriority:4,aliasEvidence:[{value:'Exact Station Description',sources:['official'],methods:['exact station id description']}]}
  ];
  await page.evaluate(rows=>{draft.channels=rows;renderCategoryReview();const list=document.createElement('div');list.id='channel-list';document.body.append(list);list.replaceChildren(...rows.map(channel=>renderLineupCard(channel)));},identityRows);
  assert.match(await page.locator('.category-review-channel-link').filter({hasText:'HBODHDP'}).textContent(),/905 · HBO Drama West \(HBODHDP\)/);
  assert.match(await page.locator('.category-review-channel-link').filter({hasText:'HBODRHD'}).textContent(),/904 · HBO Drama \(HBODRHD\)/);
  assert.doesNotMatch(await page.locator('.category-review-channel-link').filter({hasText:'HBODRHD'}).textContent(),/East/);
  assert.match(await page.locator('.category-review-channel-link').filter({hasText:'BBCAM'}).textContent(),/801 · BBC America Pacific \(BBCAM\)/);
  assert.equal(await page.locator('.category-review-channel-link').filter({hasText:'KFallback'}).textContent(),'51 · KFallback');
  assert.equal(await page.locator('.category-review-channel-link').filter({hasText:'777'}).textContent(),'777');
  assert.equal(await page.locator('.category-review-channel-link img').count(),0,'category review escapes unsafe names');
  assert.equal(await page.locator('#channel-list img').count(),0,'customization escapes unsafe names');
  for (const [id,label] of [['historical','HBO (HBOHTHD)'],['supplemental','Verified Full Name (CALL)'],['exact-description','Exact Station Description (EXACTID)']]) {
   assert.match(await page.locator('.category-review-channel-link').filter({hasText:id==='historical'?'HBOHTHD':id==='supplemental'?'CALL':'EXACTID'}).textContent(),new RegExp(label.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')));
   assert.match(await page.locator(`#channel-list .channel-name`).filter({hasText:id==='historical'?'HBOHTHD':id==='supplemental'?'CALL':'EXACTID'}).textContent(),new RegExp(label.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')));
  }
  assert.match(await page.locator('#channel-list .channel-name').filter({hasText:'HBODHDP'}).textContent(),/HBO Drama West \(HBODHDP\)/);
  assert.match(await page.locator('#channel-list .channel-name').filter({hasText:'HBODRHD'}).textContent(),/HBO Drama \(HBODRHD\)/);
  assert.match(await page.locator('#channel-list .channel-name').filter({hasText:'BBCAM'}).textContent(),/BBC America Pacific \(BBCAM\)/);
  await page.getByLabel('Select 905 HBO Drama West (HBODHDP) for confirmation').check();
  await page.getByLabel('Category for 905 HBO Drama West (HBODHDP)').selectOption('Entertainment');
  assert.deepEqual(await page.evaluate(()=>getCategorySelection()),['west'],'display names do not change stable selection identity');
  assert.deepEqual(await page.evaluate(()=>getCategoryEdits()),[['west','Entertainment']],'category edit remains keyed by channel ID');
  console.log('Category review/customization: canonical resolver, regional friendly names, fallback/escaping, stable selection/category edits, pending corrections, atomic confirmation, retry and responsive layout passed');
 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
