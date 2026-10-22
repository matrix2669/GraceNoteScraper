const {chromium}=require('playwright');
const fs=require('node:fs'),assert=require('node:assert/strict');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:process.env.PLAYWRIGHT_CHANNEL||undefined});
 try {
  const page=await browser.newPage();await page.route('**/*',r=>r.abort());
  const html=fs.readFileSync('lineuparr.html','utf8');
  await page.setContent(html.replace(/<script>[\s\S]*?<\/script>/g,''));
  const code=html.slice(html.indexOf('    async function api('),html.indexOf('    function setProviderAddressStatus')) + html.slice(html.indexOf('    async function loadDraft('),html.indexOf('    function renderAll('));
  await page.evaluate(code=>{
   window.draft={sourceFingerprint:'source',resetGeneration:4};window.dispatchBusy=false;window.saving=false;window.pendingDecisions=new Set();
   window.matchReview={};window.matchReviewLimit=200;window.matchInputSignature='old';window.posts=[];window.failure=false;
   window.message='';window.showMessage=text=>message=text;
   for(const n of ['invalidateDispatchReview','closeMatchAlternatives','setDispatchControls','scheduleWorkflowRefresh'])window[n]=()=>{};
   window.dispatchReviewEpoch=0;window.dispatchConfig={configured:false};window.renders=0;window.renderAll=()=>renders++;window.loadMatchReview=async()=>{};
   window.fetch=async(path,options={})=>{
    if(options.method==='POST'){
     posts.push({body:JSON.parse(options.body),generation:options.headers.get('X-Builder-Generation')});
     if(failure)return new Response('Builder choices changed',{status:409});
     return Response.json({backup:'new-recovery',restoreAvailable:true,generation:5});
    }
    if(path==='/api/lineuparr/draft')return Response.json({sourceFingerprint:'source',resetGeneration:4});
    return Response.json({sourceFingerprint:'source',token:'state-token',generation:4,recovery:{id:'restart-recovery'},clears:['builder choices'],preserves:['guide','TMDB','connection credentials']});
   };
   (0,eval)(code+'\nwindow.resetBuilder=resetBuilder;window.refreshResetRecovery=refreshResetRecovery;window.loadDraft=loadDraft;');
  },code);
  await page.evaluate(()=>loadDraft());
  await page.waitForFunction(()=>!document.getElementById('builder-reset-undo').hidden);
  assert.equal(await page.evaluate(()=>renders),1);
  assert.equal(await page.locator('#builder-reset-undo').evaluate(button=>button.hidden),false);
  async function open(restore=false) {
   await page.evaluate(restore=>{window.operation=resetBuilder(restore)},restore);
   await page.locator('#builder-reset-dialog').waitFor({state:'visible'});
   assert.equal(await page.locator('#builder-reset-dialog input[value="keep"]').isChecked(),true);
  }
  await open();
  // A second invocation while this confirmation is open must be a no-op.
  await page.evaluate(()=>resetBuilder());
  await page.locator('#builder-reset-cancel').click();await page.evaluate(()=>operation);
  assert.equal(await page.evaluate(()=>posts.length),0);
  await open();await page.keyboard.press('Escape');await page.evaluate(()=>operation);
  assert.equal(await page.evaluate(()=>posts.length),0);
  await page.evaluate(()=>{failure=true});await open(true);
  await page.locator('#builder-reset-dialog input[value="withdraw"]').check();
  await page.locator('#builder-reset-confirm').click();await page.evaluate(()=>operation);
  assert.match(await page.evaluate(()=>message),/choices changed/);
  assert.equal(await page.evaluate(()=>draft.resetGeneration),4);
  assert.equal(await page.evaluate(()=>posts.at(-1).body.withdrawPublished),true);
  await page.evaluate(()=>{failure=false});await open(true);
  // Each new attempt defaults to keep, including after a failed withdrawal.
  await page.locator('#builder-reset-confirm').click();await page.evaluate(()=>operation);
  let post=await page.evaluate(()=>posts.at(-1));
  assert.equal(post.generation,'4');assert.equal(post.body.restore,'restart-recovery');assert.equal(post.body.confirm,true);
  assert.equal(post.body.withdrawPublished,false);assert.equal(await page.evaluate(()=>matchReview),null);
  await open();await page.locator('#builder-reset-dialog input[value="withdraw"]').check();
  await page.locator('#builder-reset-confirm').click();await page.evaluate(()=>operation);
  post=await page.evaluate(()=>posts.at(-1));assert.equal(post.body.withdrawPublished,true);
  assert.match(await page.evaluate(()=>message),/withdrawn until regeneration/);
  console.log('Browser reset: initial-load recovery, cancel/Escape, duplicate invocation, stale failure, keep/withdraw, restore and generation header pass');

 } finally {await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
