// Run with Playwright installed; this test never contacts a provider.
const {chromium}=require('playwright');
const fs=require('node:fs'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL || undefined});
 try {
  const page=await browser.newPage();
  await page.route('**/*',route=>route.abort());
  const html=fs.readFileSync('setup.html','utf8').replace(/<script>[\s\S]*?<\/script>/g,'');
  await page.setContent(html);
  await page.evaluate(()=>{
   document.querySelector('#currentCard').style.display='flex';
   document.querySelector('#currentName').textContent='Example cable provider — Digital';
   document.querySelector('#xmltvGuideLink').textContent='http://guide.example:8080/xmlguide.xmltv';
   document.querySelector('#internalXMLTVRow').hidden=false;
   document.querySelector('#internalXMLTVLink').textContent='http://gracenotescraper:8080/xmlguide.xmltv';
   // The builder contribution adds this third action in the composed app.
   const link=document.createElement('a');link.className='guide-link';link.style.display='inline-block';link.textContent='Build Lineuparr JSON';
   document.querySelector('.current-actions').append(link);
  });
  for(const width of [320,390,720,768,1080,1440]) {
   await page.setViewportSize({width,height:900});
   const result=await page.evaluate(()=>({overflow:document.documentElement.scrollWidth>innerWidth,buttons:[...document.querySelectorAll('.current-actions > *')].map(el=>({width:el.clientWidth,scroll:el.scrollWidth,height:el.getBoundingClientRect().height,whiteSpace:getComputedStyle(el).whiteSpace}))}));
   assert.equal(result.overflow,false,`page overflow at ${width}`);
   for(const button of result.buttons){assert.equal(button.whiteSpace,'nowrap');assert.equal(button.height,42);assert.ok(button.scroll<=button.width,`button overflow at ${width}`)}
  }
  assert.equal(await page.locator('#postalCode').getAttribute('placeholder'),'10001');
  assert.equal(await page.locator('#internalBaseURL').getAttribute('placeholder'),'http://gracenotescraper:8080');
  console.log('Setup layout: six widths, three uncompressed actions, both URLs and generic placeholders passed');
 } finally {await browser.close()}
})().catch(error=>{console.error(error);process.exitCode=1});
