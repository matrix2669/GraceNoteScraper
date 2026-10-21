// Real Chromium DOM/dialog/fetch checks; every request is intercepted locally.
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const { chromium } = require('playwright');

const html = fs.readFileSync(path.join(__dirname, '../lineuparr.html'), 'utf8');
function section(start, end) {
  const a = html.indexOf(start), b = html.indexOf(end, a);
  assert(a >= 0 && b > a, `Missing production section: ${start}`);
  return html.slice(a, b);
}
const markup = section('        <button type="button" id="affiliate-preview"', '        <div class="match-list"');
const handlers = section('    let affiliatePreview = null;', '    function renderMatchReview()');
const api = section('    async function api(', '    function setProviderAddressStatus(');
const controls = section('    function setDispatchControls()', '    function showConfirmedMatches()');
const listeners = section("    document.getElementById('affiliate-preview').addEventListener", '    els.matchConfirmedJump.addEventListener');
const preview = {token:'fixture-token', manualCount:3, rows:[
  {key:'a',group:'station\u0000ktvt',channelNumber:'502',channelName:'WCBSDT',streamName:'US: CBS (KTVT)',accountId:3,reason:'KTVT differs from WCBS'},
  {key:'b',group:'station\u0000ktvt',channelNumber:'502',channelName:'WCBSDT',streamName:'US: CBS (KTVT) HD',accountId:4,reason:'KTVT differs from WCBS'},
  {key:'c',group:'station\u0000wbz',channelNumber:'502',channelName:'WCBSDT',streamName:'US: CBS (WBZ)',accountId:3,reason:'WBZ differs from WCBS'},
]};

async function main() {
  const browser = await chromium.launch({headless:true, ...(process.env.PLAYWRIGHT_CHANNEL ? {channel:process.env.PLAYWRIGHT_CHANNEL} : {})});
  try {
    const page = await browser.newPage();
    page.setDefaultTimeout(10000);
    const requests = [], errors = [];
    let failApply = false, release = null, holdMethod = null;
    const names = ['dispatchURL','dispatchAuth','dispatchSave','dispatchRefresh','dispatchDisconnect','dispatchUser','dispatchPassword','dispatchAPIKey'];
    page.on('pageerror', e => errors.push(e.message));
    await page.route('**/*', async route => {
      const request = route.request();
      assert.equal(new URL(request.url()).origin, 'http://affiliate.test', 'Unexpected external request');
      if (request.isNavigationRequest()) {
        await route.fulfill({contentType:'text/html', body:`${markup}${names.map(id=>`<input id="${id}">`).join('')}`});
        return;
      }
      assert.equal(new URL(request.url()).pathname, '/api/lineuparr/dispatcharr/affiliate-exclusions');
      requests.push({method:request.method(), headers:request.headers(), body:request.postData()});
      if (request.method() === holdMethod) await new Promise(resolve => { release = resolve; });
      await route.fulfill({status:request.method()==='POST' && failApply ? 409 : 200,
        contentType:request.method()==='POST' && failApply ? 'text/plain' : 'application/json',
        body:request.method()==='POST' ? (failApply ? 'Review changed; preview again' : JSON.stringify({saved:2})) : JSON.stringify(preview)});
    });
    await page.goto('http://affiliate.test/');
    await page.addScriptTag({content:`
      let dispatchBusy=false, dispatchReviewEpoch=0, decisionRevision=0;
      const els=Object.fromEntries(${JSON.stringify(names)}.map(id=>[id,document.getElementById(id)]));
      window.testLoads=[]; window.testMessages=[];
      function showMessage(...args){window.testMessages.push(args);}
      async function loadMatchReview(...args){window.testLoads.push(args);}
      async function loadDraft(){window.testLoads.push('draft');}
      ${api}\n${controls}\n${handlers}\n${listeners}
    `});
    const checks = page.locator('#affiliate-rows input');
    const apply = page.locator('#affiliate-apply');
    async function open() {
      assert.equal(await page.locator('#affiliate-preview').isVisible(), true, 'Preview button must be visible in fixture');
      assert.equal(await page.locator('#affiliate-preview').isEnabled(), true, 'Preview button must be enabled in fixture');
      await page.locator('#affiliate-preview').click();
      try {
        await page.waitForFunction(() => document.querySelectorAll('#affiliate-rows input').length === 3 && !document.getElementById('affiliate-preview').disabled, null, {polling:50});
      } catch (error) {
        console.error({requests, errors, messages:await page.evaluate(()=>window.testMessages)});
        throw error;
      }
    }
    async function waitIdle() { await page.waitForFunction(() => !document.getElementById('affiliate-preview').disabled); }
    async function waitHeld() {
      for (let i=0; i<1000 && !release; i++) await new Promise(resolve=>setTimeout(resolve,5));
      assert(release, 'Mock request did not reach its controlled boundary');
    }
    async function confirmApply(accept) {
      const dialog = page.waitForEvent('dialog');
      const click = apply.click();
      const d = await dialog;
      assert.equal(d.type(), 'confirm');
      assert.match(d.message(), /individual channel-scoped exclusions/);
      await (accept ? d.accept() : d.dismiss());
      await click;
      await waitIdle();
    }
    await open();
    assert.equal(requests.length, 1);
    assert.equal(requests[0].method, 'GET');
    assert.equal(await checks.evaluateAll(xs=>xs.filter(x=>x.checked).length), 0);
    assert.equal(await apply.isDisabled(), true);
    await checks.nth(0).check();
    assert.deepEqual(await checks.evaluateAll(xs=>xs.map(x=>x.checked)), [true,true,false]);
    await page.locator('#affiliate-select-all').click();
    assert.equal(await checks.evaluateAll(xs=>xs.filter(x=>x.checked).length), 3);
    await page.locator('#affiliate-select-none').click();
    assert.equal(await apply.isDisabled(), true);
    await checks.nth(0).check();
    await confirmApply(false);
    assert.equal(requests.filter(r=>r.method==='POST').length, 0, 'Dismissed confirmation saved decisions');
    await confirmApply(true);
    const post = requests.find(r=>r.method==='POST');
    assert.equal(post.headers['content-type'], 'application/json');
    assert.deepEqual(JSON.parse(post.body), {token:'fixture-token', keys:['a','b']});
    assert.deepEqual(await page.evaluate(()=>window.testLoads), [[false,false],'draft']);
    assert.equal(await page.locator('#affiliate-review').isVisible(), false);

    await open(); await checks.nth(0).check(); failApply=true;
    await confirmApply(true);
    assert.equal(await page.locator('#affiliate-review').isVisible(), false);
    assert.deepEqual(await page.evaluate(()=>window.testLoads), [[false,false],'draft']);
    assert.match((await page.evaluate(()=>window.testMessages)).at(-1)[0], /Review changed/);
    failApply=false;

    holdMethod='GET'; release=null;
    await page.locator('#affiliate-preview').click();
    await page.waitForFunction(() => document.getElementById('affiliate-preview').disabled);
    await waitHeld();
    await page.evaluate(()=>{dispatchReviewEpoch++;});
    release(); await waitIdle();
    assert.equal(await page.locator('#affiliate-review').isVisible(), false, 'Obsolete preview installed');
    holdMethod=null;

    await open(); await checks.nth(0).check(); holdMethod='POST'; release=null;
    const dialog = page.waitForEvent('dialog'); const click = apply.click();
    await (await dialog).accept(); await click;
    await waitHeld();
    await page.evaluate(()=>{dispatchReviewEpoch++;});
    release(); await waitIdle();
    assert.deepEqual(await page.evaluate(()=>window.testLoads), [[false,false],'draft'], 'Obsolete apply reloaded another source');
    assert.deepEqual(errors, []);
    console.log('Affiliate Chromium: real DOM groups, no preselection, confirmation/cancel, JSON POST, stale apply and late response guards passed');
  } finally { await browser.close(); }
}
main().catch(error=>{console.error(error);process.exitCode=1;});
