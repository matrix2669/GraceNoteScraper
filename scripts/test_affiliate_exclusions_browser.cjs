// Production renderer in a real browser; no server or external request needed.
const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict');
const {chromium}=require('playwright');
const html=fs.readFileSync(path.join(__dirname,'../lineuparr.html'),'utf8');
const a=html.indexOf('        <details id="automatic-exclusions">'),b=html.indexOf('        <div class="match-list"',a);
const c=html.indexOf('    function renderAutomaticExclusions('),d=html.indexOf('    function renderMatchReview()',c);
assert(a>=0&&b>a&&c>=0&&d>c);
(async()=>{const browser=await chromium.launch({headless:true,...(process.env.PLAYWRIGHT_CHANNEL?{channel:process.env.PLAYWRIGHT_CHANNEL}:{})});try{
 const page=await browser.newPage(),errors=[],requests=[];page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>requests.push(r.url()));
 await page.setContent(html.slice(a,b));
 await page.addScriptTag({content:`function emptyMessage(text){const p=document.createElement('p');p.textContent=text;return p;}\n${html.slice(c,d)}`});
 const rows=[{channelNumber:'502',channelName:'WCBSDT',streamName:'US: CBS (KTVT) <script>bad()</script>',accountId:3,exported:true,reason:'Conflicting callsign'}, {channelNumber:'901',channelName:'HBOHDP',streamName:'US: HBO HD',accountId:7,exported:true,pendingReview:true,reason:'Unknown feed'}, {channelNumber:'901',channelName:'HBOHDP',streamName:'US: HBO East',accountId:3,conflict:true,reason:'Opposing feed'}];
 await page.evaluate(rows=>renderAutomaticExclusions({rows,message:'Snapshot current'}),rows);
 await page.locator('summary').click();
 assert.equal(await page.locator('#automatic-rows p').count(),3);
 assert.match(await page.locator('#automatic-count').innerText(),/2 exclusions, 1 manual, 1 confirmation conflicts/);
 assert.match(await page.locator('#automatic-rows').innerText(),/Automatically excluded/);
 assert.match(await page.locator('#automatic-rows').innerText(),/Manual review required/);
 assert.match(await page.locator('#automatic-rows').innerText(),/Saved confirmation preserved/);
 assert.equal(await page.locator('#automatic-rows script').count(),0);
 assert.equal(await page.locator('input,button').count(),0,'No bulk confirmation burden');
 await page.evaluate(()=>renderAutomaticExclusions(null));assert.equal(await page.locator('#automatic-rows p').count(),0);
 assert.match(await page.locator('#automatic-message').innerText(),/Refresh streams/);assert.deepEqual(errors,[]);assert.deepEqual(requests,[]);
 console.log('Automatic browser: real DOM, no controls/writes/network, escaped names, pending/conflict states and stale clearing passed');
}finally{await browser.close();}})().catch(e=>{console.error(e);process.exitCode=1;});
