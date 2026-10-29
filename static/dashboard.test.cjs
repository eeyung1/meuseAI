const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

class Element {
  constructor() {
    this.textContent = '';
    this.children = [];
    this.listeners = {};
    this.attributes = {};
    this.dataset = {};
    this.style = {};
    this.hidden = false;
    this.disabled = false;
    this.value = '';
    this.className = '';
    this.parentElement = { classList: { toggle() {} } };
    this.classList = {
      toggle() {},
      add: cls => { if (!this.className.includes(cls)) this.className += ' ' + cls; },
      remove: cls => { this.className = this.className.replace(cls, '').trim(); },
      contains: cls => this.className.includes(cls)
    };
  }
  focus() { this.focused = true; }
  showModal() { this.open = true; }
  close() { this.open = false; this.listeners.close?.(); }
  select() { this.selected = true; }
  set innerHTML(_) { throw new Error('Untrusted HTML rendering'); }
  addEventListener(type, fn) { this.listeners[type] = fn; }
  setAttribute(name, value) { this.attributes[name] = value; }
  append(...children) { this.children.push(...children); }
  replaceChildren(...children) { this.children = children; }
}
function profile() {
  return { full_name: '<img src=x onerror=alert(1)>', xp: 250, streak_days: 2, rank: 1, funnel_tier: 'free_week',
    modules: ['day_1', 'day_2', 'day_3', 'day_4', 'tier_1'].map((id, i) => ({ id, title: `Module ${i + 1}`, state: i === 0 ? 'completed' : i === 1 ? 'available' : 'locked', description: 'Practice', prerequisite: 'Previous day', xp: i < 4 ? 250 : 0 })),
    leaderboard: [{ label: 'You', xp: 250, rank: 1, you: true }], activity: [{ title: '<script>not HTML</script>', at: '2026-10-28T12:00:00Z' }] };
}
async function settle() { for (let i = 0; i < 4; i++) await new Promise(resolve => setImmediate(resolve)); }
function harness(handler) {
  const elements = new Map(), streams = [], calls = [], timers = [], windows = {};
  const element = id => { if (!elements.has(id)) elements.set(id, new Element()); return elements.get(id); };
  const nodes = ['day_1', 'day_2', 'day_3', 'day_4', 'tier_1'].map(id => { const e = new Element(); e.dataset.module = id; return e; });
  const storage = new Map();
  const context = { navigator: { clipboard: { writeText: async () => {} } }, localStorage: { getItem:k=>storage.get(k), setItem:(k,v)=>storage.set(k,v), removeItem:k=>storage.delete(k) }, document: { getElementById: element, querySelectorAll: () => nodes, createElement: () => new Element(), addEventListener() {}, hidden: false },
    window: { addEventListener(type, fn) { windows[type] = fn; } }, URL, location: { assign(value) { context.destination = value; } },
    fetch: async (path, options) => { calls.push({ path, options }); return handler(path, options); },
    EventSource: class { constructor(path) { this.path = path; this.listeners = {}; streams.push(this); } addEventListener(type, fn) { this.listeners[type] = fn; } close() { this.closed = true; } },
    setTimeout(fn) { timers.push(fn); return timers.length; }, clearTimeout() {}, setInterval() { return 1; }, clearInterval() {} };
  vm.runInNewContext(fs.readFileSync(__dirname + '/dashboard.js', 'utf8'), context);
  return { element, nodes, streams, calls, timers, windows, context, storage };
}
const ok = value => ({ ok: true, status: 200, json: async () => value });

test('renders server state safely, selects locked modules and reconnects', async () => {
  const h = harness(() => ok(profile())); await settle();
  assert.equal(h.element('xp').textContent, '250');
  assert.equal(h.element('student-name').textContent, '<img src=x onerror=alert(1)>');
  assert.equal(h.element('activity-list').children[0].children[0].textContent, '<script>not HTML</script>');
  assert.equal(h.element('module-title').textContent, 'Module 2');
  h.nodes[2].listeners.click(); assert.equal(h.element('submission-form').hidden, true);
  assert.equal(h.element('module-state').textContent, 'Complete previous lesson');
  assert.equal(h.streams.length, 1); h.streams[0].onopen(); await settle();
  assert.equal(h.element('connection').textContent, 'Live updates');
  h.streams[0].onerror(); assert.equal(h.element('connection').textContent, 'Reconnecting');
  h.streams[0].listeners.rank(); h.timers.at(-1)(); await settle();
  assert.equal(h.element('xp').textContent, '250');
  h.windows.pagehide(); assert.equal(h.streams[0].closed, true);
  h.windows.pageshow({ persisted: true }); await settle(); assert.equal(h.streams.length, 2);
});

test('submits selected day and applies XP only after confirmed snapshot', async () => {
  let saved = false; const h = harness((path, options) => {
    if (path === '/api/v1/curriculum/complete') { assert.deepEqual(JSON.parse(options.body), { day: 2, submission: 'My image prompt\n\nReflection: The side lighting changed the mood.' }); saved = true; return ok({}); }
    const data = profile(); if (saved) { data.xp = 500; data.modules[1].state = 'completed'; data.modules[2].state = 'available'; } return ok(data);
  }); await settle(); h.element('reflection').value = 'The side lighting changed the mood.'; h.element('submission').value = ' My image prompt ';
  const promise = h.element('submission-form').listeners.submit({ preventDefault() {} });
  assert.equal(h.element('xp').textContent, '250'); await promise; await settle();
  assert.equal(h.element('xp').textContent, '500'); assert.equal(h.element('module-title').textContent, 'Module 3');
});

test('failed submissions retain prompt and do not invent progress', async () => {
  const h = harness(path => path === '/api/v1/curriculum/complete' ? { ok: false, status: 503 } : ok(profile())); await settle();
  h.element('reflection').value = 'The side lighting changed the mood.'; h.element('submission').value = 'Unsaved exercise'; await h.element('submission-form').listeners.submit({ preventDefault() {} });
  assert.equal(h.element('xp').textContent, '250'); assert.equal(h.element('submission').value, 'Unsaved exercise');
  assert.match(h.element('notice').textContent, /try again/); assert.equal(h.element('submit-button').disabled, false);
});

test('expired session never starts SSE, and failed initial fetch can recover', async () => {
  const expired = harness(() => ({ ok: false, status: 401 })); await settle();
  assert.equal(expired.streams.length, 0); assert.match(expired.element('notice').textContent, /sign in again/);
  let fail = true; const recovery = harness(() => fail ? { ok: false, status: 503 } : ok(profile())); await settle(); assert.equal(recovery.streams.length, 0);
  fail = false; await recovery.element('refresh-button').listeners.click(); await settle(); assert.equal(recovery.streams.length, 1);
});

test('checkout accepts only Paystack HTTPS destination', async () => {
  for (const destination of ['https://checkout.paystack.com/code', 'https://attacker.example/code', 'http://checkout.paystack.com/code']) {
    const h = harness(path => { if (path === '/api/v1/payments/checkout') return ok({ authorization_url: destination }); const data = profile(); data.modules[4].state = 'available'; return ok(data); }); await settle();
    h.nodes[4].listeners.click(); await h.element('checkout-button').listeners.click();
    if (destination.startsWith('https://checkout.paystack.com/')) assert.equal(h.context.destination, destination);
    else { assert.equal(h.context.destination, undefined); assert.match(h.element('notice').textContent, /Checkout is unavailable/); }
  }
});

test('studio switches between selfie and scratch modes and populates preset prompts', async () => {
  const h = harness(() => ok(profile())); await settle();
  assert.equal(h.element('gemini-studio').hidden, false);
  assert.match(h.element('gemini-prompt').value, /reference image|recognizable/i);

  // Switch to creative scratch mode
  h.element('mode-scratch').listeners.click();
  assert.match(h.element('gemini-prompt').value, /subject|lighting/i);

  // Switch back to selfie mode
  h.element('mode-selfie').listeners.click();
  assert.match(h.element('gemini-prompt').value, /reference image|recognizable/i);
});

test('submitting social share link posts to /api/v1/curriculum/share-link', async () => {
  let submittedPayload = null;
  const h = harness((path, options) => {
    if (path === '/api/v1/curriculum/share-link') {
      submittedPayload = JSON.parse(options.body);
      return ok({ status: 'pending', message: 'Submitted for verification' });
    }
    const data = profile();
    return ok(data);
  });
  await settle();

  // Complete day 2 to trigger celebration
  h.element('reflection').value = 'The side lighting changed the mood.'; h.element('submission').value = 'Test prompt';
  await h.element('submission-form').listeners.submit({ preventDefault() {} });
  await settle();

  assert.equal(h.element('social-celebration').hidden, false);

  // Submit social share link
  h.element('share-post-url').value = 'https://www.facebook.com/student/posts/12345';
  await h.element('share-link-form').listeners.submit({ preventDefault() {} });
  await settle();

  assert.deepEqual(submittedPayload, {
    day: 2,
    share_url: 'https://www.facebook.com/student/posts/12345',
    platform: 'social'
  });
  assert.match(h.element('share-link-status').textContent, /Submitted for verification/);
});

test('applies 10% discount to checkout button after completing the course', async () => {
  const h = harness(() => {
    const data = profile();
    data.xp = 1000;
    data.discount_percent = 10;
    data.tier_1_price_naira = 4500;
    data.modules[4].state = 'available';
    return ok(data);
  });
  await settle();

  assert.match(h.element('discount-banner-text').textContent, /10% Course Discount Unlocked/);
  assert.match(h.element('checkout-button').textContent, /4,500/);
});


 test('clipboard failure gives a manual fallback and never claims success', async () => {
 const h = harness(() => ok(profile())); await settle();
 h.context.navigator.clipboard.writeText = async () => { throw new Error('denied'); };
 await h.element('btn-copy-prompt').listeners.click();
 assert.match(h.element('studio-copied-note').textContent, /blocked.*manually/);
 assert.equal(h.element('gemini-prompt').selected, true);
 });
 test('drafts survive switching lessons, and completed lessons remain usable', async () => {
 const h = harness(() => ok(profile())); await settle();
 h.element('gemini-prompt').value = 'My own saved prompt';
 h.element('gemini-prompt').listeners.input();
 h.nodes[0].listeners.click();
 assert.equal(h.element('gemini-studio').hidden, false);
 assert.equal(h.element('saved-work').hidden, false);
 h.nodes[1].listeners.click();
 assert.equal(h.element('gemini-prompt').value, 'My own saved prompt');
 });
 test('scheduled lesson has a release time and cannot be submitted', async () => {
 const h = harness(() => { const p = profile(); p.modules[1].state='scheduled';p.modules[1].unlocks_at=new Date(Date.now()+3600000).toISOString(); return ok(p); }); await settle();
 assert.equal(h.element('submission-form').hidden, true);
 assert.match(h.element('module-help').textContent, /opens/);
 assert.equal(h.element('gemini-studio').hidden, true);
 });


test('Prompt Lab opens on demand, closes with Escape, and preserves the draft', async () => {
 const h = harness(() => ok(profile())); await settle();
 assert.equal(h.element('prompt-lab-dialog').hidden, true);
 assert.equal(h.element('open-prompt-lab').disabled, false);
 h.element('open-prompt-lab').listeners.click();
 assert.equal(h.element('prompt-lab-dialog').open, true);
 assert.equal(h.element('prompt-lab-dialog').hidden, false);
 assert.equal(h.element('open-prompt-lab').attributes['aria-expanded'], 'true');
 h.element('gemini-prompt').value = 'My working draft';
 h.element('gemini-prompt').listeners.input();
 let cancelled=false;
 h.element('prompt-lab-dialog').listeners.cancel({preventDefault(){cancelled=true;}});
 assert.equal(cancelled,true);
 assert.equal(h.element('prompt-lab-dialog').hidden,true);
 assert.equal(h.element('open-prompt-lab').focused,true);
 h.element('open-prompt-lab').listeners.click();
 assert.equal(h.element('gemini-prompt').value,'My working draft');
});

test('selecting a locked lesson closes the lab and prevents reopening it', async () => {
 const h = harness(() => ok(profile())); await settle();
 h.element('open-prompt-lab').listeners.click();
 h.nodes[2].listeners.click();
 assert.equal(h.element('prompt-lab-dialog').hidden,true);
 assert.equal(h.element('open-prompt-lab').disabled,true);
 h.element('open-prompt-lab').listeners.click();
 assert.equal(h.element('prompt-lab-dialog').hidden,true);
});

test('dashboard keeps practice controls without image uploads or gallery', () => {
 const html=fs.readFileSync(__dirname+'/dashboard.html','utf8');
 assert.ok(!/showcase|type="file"/.test(html));
 const button=html.indexOf('id="open-prompt-lab"'),title=html.indexOf('TODAY’S LEARNING');
 assert.ok(button<title);
 const dialog=html.indexOf('<dialog id="prompt-lab-dialog"');
 for(const id of ['gemini-studio','submission-form','reflection','submit-button'])assert.ok(html.indexOf('id="'+id+'"')>dialog);
});
