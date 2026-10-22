const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const html = fs.readFileSync('lineuparr.html', 'utf8');
const section = (start, end) => html.slice(html.indexOf(start), html.indexOf(end, html.indexOf(start)));
const production = require('./review_fixture_helpers.cjs')(html) +
  section('    async function loadMatchReview(', '    function renderAutomaticExclusions(') +
  section('    function workflowStates(', '    function renderGuidedWorkflow(');
const tick = () => new Promise(resolve => setImmediate(resolve));
const view = (count, limit = 100) => ({operationScope:'scope', reviewStatus:'current', candidateCount:count,
  candidates:count ? [{key:'restored'}] : [], decisions:[], visibleLimit:limit});
const receipt = state => ({operation:'saved-op', key:'saved', decision:'confirmed', state});

function fixture() {
  const c = {
    URLSearchParams, Map, Set, encodeURIComponent,
    draft:{sourceFingerprint:'fixture', channels:[]}, dispatchConfig:{configured:true},
    pendingDecisions:new Set(), dispatchBusy:false, builderResetPending:false,
    matchReviewLimit:100, matchReviewRequest:0, decisionRevision:0, dispatchReviewEpoch:0,
    dispatchBusyRequest:0, workflowRemote:{dispatchConfig:{configured:true}}, workflowProgress:{},
    dispatchConnectionError:false, document:{getElementById(){return {}}, querySelector(){return {}}},
    matchReview:null, reviewOrder:new Map(), reviews:[], receipts:[], renders:[], workflowRenders:0,
    showMessage(){}, setDispatchControls(){},
    renderMatchReview(){c.renders.push(c.matchReview)},
    renderGuidedWorkflow(){c.workflowRenders++},
    api:path => new Promise(resolve => {
      if (path.includes('/review?')) c.reviews.push(resolve);
      else c.receipts.push(resolve);
    })
  };
  vm.createContext(c);
  vm.runInContext(production, c);
  c.matchActionState('saved', 'uncertain', {operation:'saved-op', decision:'confirmed', scope:'scope', source:'fixture'});
  return c;
}

function currentState(c, ready) {
  const current = view(150, 200);
  c.matchReview = current;
  c.workflowRemote.dispatch = current;
  c.workflowRemote.dispatchReady = ready;
  c.matchReviewLimit = 200;
  c.reviewOrder = new Map([['current-first', 0], ['current-last', 1]]);
  return {current, order:c.reviewOrder, ready, renderCount:c.renders.length, workflowRenders:c.workflowRenders};
}

function assertCurrent(c, expected) {
  assert.strictEqual(c.matchReview, expected.current, 'stale read replaced the visible view');
  assert.strictEqual(c.workflowRemote.dispatch, expected.current, 'stale read replaced workflow counts');
  assert.equal(c.workflowRemote.dispatchReady, expected.ready, 'stale read changed workflow readiness');
  assert.strictEqual(c.reviewOrder, expected.order, 'stale read reset the current order');
  assert.equal(c.matchReviewLimit, 200, 'stale read reset the current window');
  assert.equal(c.renders.length, expected.renderCount, 'stale read rendered');
  assert.equal(c.workflowRenders, expected.workflowRenders, 'stale read rendered workflow');
  assert.equal(c.workflowStates().find(state => state.step === 6).complete, false, 'stale zero-count view completed Step6');
}

(async () => {
  // Exact independent reproduction: A waits for a receipt, B publishes150,
  // then A's normal false-return reconciliation must not publish zero.
  for (const state of ['saved', 'undone', 'superseded']) {
    const c = fixture();
    const old = c.loadMatchReview(false, false, 100);
    c.reviews[0](view(0)); await tick();
    const newer = c.loadMatchReview(false, false, 200);
    c.reviews[1](view(150, 200)); await tick();
    assert.equal(c.receipts.length, 2);
    c.receipts[1](receipt(state)); await newer;
    const expected = {current:c.matchReview, order:c.reviewOrder, ready:true,
      renderCount:c.renders.length, workflowRenders:c.workflowRenders};
    c.receipts[0](receipt(state)); await old;
    assertCurrent(c, expected);
  }

  // Every fence can advance independently while the receipt is pending. Check
  // both outcomes, including not-saved: stale work must not issue its next GET.
  for (const fence of ['matchReviewRequest', 'decisionRevision', 'dispatchReviewEpoch']) {
    for (const state of ['saved', 'undone', 'superseded', 'not-saved']) {
      const c = fixture();
      const old = c.loadMatchReview(false, false, 100);
      c.reviews[0](view(0)); await tick();
      c[fence]++;
      const expected = currentState(c, false);
      c.receipts[0](receipt(state)); await old;
      assert.equal(c.reviews.length, 1, `${fence}/${state}: stale reconciliation issued a follow-up GET`);
      assertCurrent(c, expected);
    }

    // Retain the existing second fence after a genuinely needed follow-up GET.
    const c = fixture();
    const old = c.loadMatchReview(false, false, 100);
    c.reviews[0](view(0)); await tick();
    c.receipts[0](receipt('not-saved')); await tick();
    assert.equal(c.reviews.length, 2);
    c[fence]++;
    const expected = currentState(c, false);
    c.reviews[1](view(0)); await old;
    assertCurrent(c, expected);
  }
  console.log('Receipt races: overlapping noninteractive loads, all three post-await fences, both reconciliation outcomes, stale follow-up GETs and actual Step6 completion passed (18 schedules)');
})().catch(error => {console.error(error); process.exitCode = 1});
