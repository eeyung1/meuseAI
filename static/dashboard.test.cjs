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
    this.canvasText = [];
    this.canvasContext = {
      createLinearGradient: () => ({ addColorStop() {} }),
      fillRect() {},
      fillText: text => this.canvasText.push(String(text))
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
  getContext(type) { return type === '2d' ? this.canvasContext : null; }
  toDataURL() { this.dataURLRequested = true; return 'data:image/png;base64,badge'; }
  click() { this.clicked = true; }
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
  const context = { navigator: { clipboard: { writeText: async () => {} }, share: async value => { context.shared = value; } }, localStorage: { getItem:k=>storage.get(k), setItem:(k,v)=>storage.set(k,v), removeItem:k=>storage.delete(k) }, document: { getElementById: element, querySelectorAll: () => nodes, createElement: () => new Element(), addEventListener() {}, hidden: false },
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
    if (path === '/api/v1/curriculum/complete') { assert.deepEqual(JSON.parse(options.body), { day: 2, submission: 'My image prompt' }); saved = true; return ok({}); }
    const data = profile(); if (saved) { data.xp = 550; data.modules[1].state = 'completed'; data.modules[2].state = 'available'; } return ok(data);
  }); await settle(); h.element('gemini-prompt').value = ' My image prompt ';
  const promise = h.element('submission-form').listeners.submit({ preventDefault() {} });
  assert.equal(h.element('xp').textContent, '250'); await promise; await settle();
  assert.equal(h.element('xp').textContent, '550'); assert.equal(h.element('module-title').textContent, 'Module 3');
});

test('failed submissions retain prompt and do not invent progress', async () => {
  const h = harness(path => path === '/api/v1/curriculum/complete' ? { ok: false, status: 503 } : ok(profile())); await settle();
  h.element('gemini-prompt').value = 'Unsaved exercise'; await h.element('submission-form').listeners.submit({ preventDefault() {} });
  assert.equal(h.element('xp').textContent, '250'); assert.equal(h.element('gemini-prompt').value, 'Unsaved exercise');
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

test('story badge renders, downloads and grants instant sharing XP once', async () => {
  let submittedPayload = null, shared = false;
  const h = harness((path, options) => {
    if (path === '/api/v1/curriculum/share-link') {
      submittedPayload = JSON.parse(options.body);
      shared = true;
      return ok({ status: 'approved', reward_granted: true, xp_awarded: 250 });
    }
    const data = profile();
    if (shared) { data.xp = 500; data.social_shares = [{ day: 1, status: 'approved', xp_awarded: 250 }]; }
    return ok(data);
  });
  await settle();
  h.nodes[0].listeners.click();
  assert.equal(h.element('social-celebration').hidden, false);
  assert.match(h.element('story-badge-canvas').canvasText.join(' '), /DAY 1.*250 XP.*<img src=x/);
  h.element('download-story-badge').listeners.click();
  assert.equal(h.element('story-badge-canvas').dataURLRequested, true);
  await h.element('share-whatsapp-btn').listeners.click(); await settle();
  assert.deepEqual(submittedPayload, {
    day: 1,
    share_url: 'https://meuseai.onrender.com/share?day=1&student=%3Cimg%20src%3Dx%20onerror%3Dalert(1)%3E',
    platform: 'whatsapp',
    share_triggered: true
  });
  assert.match(h.context.destination, /^https:\/\/wa\.me\/\?text=/);
  assert.match(h.element('story-share-status').textContent, /250 sharing XP|250 XP/);
  assert.equal(h.element('xp').textContent, '500');
  await h.element('share-instagram-btn').listeners.click(); await settle();
  assert.match(h.context.shared.url, /\/share\?day=1/);
  await h.element('share-facebook-btn').listeners.click(); await settle();
  assert.match(h.context.destination, /^https:\/\/www\.facebook\.com\/sharer/);
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
  h.element('track-video').checked=true;h.element('track-video').listeners.change();
  assert.match(h.element('track-total').textContent, /7,200/);
  assert.match(h.element('bundle-saving').textContent, /save ₦1,800/);
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
 test('available next lesson advertises the same-day momentum bonus', async () => {
  const h = harness(() => ok(profile())); await settle();
  assert.equal(h.element('submission-form').hidden, false);
  assert.match(h.element('module-help').textContent, /Available now.*\+50 bonus XP/);
  assert.equal(h.element('gemini-studio').hidden, false);
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
 for(const id of ['gemini-studio','submission-form','submit-button'])assert.ok(html.indexOf('id="'+id+'"')>dialog);
});

test('every lesson offers ten distinct prompts in both modes, matched to its skill', async () => {
 const goals=[/changing only the setting/,/Change only the light/,/consistent visual anchors/,/two separate follow-up edits/];
 const all=new Set();
 for(let day=0;day<4;day++) {
  const h=harness(()=>{const p=profile();p.modules.forEach(m=>{if(m.id.startsWith('day_'))m.state='available';});return ok(p);});
  await settle();h.nodes[day].listeners.click();
  for(const mode of ['mode-scratch','mode-selfie']) {
   h.element(mode).listeners.click();
   const buttons=h.element('studio-chips').children;
   assert.equal(buttons.length,10);
   assert.equal(new Set(buttons.map(b=>b.textContent)).size,10);
   const texts=[];
   for(const button of buttons){button.listeners.click();const text=h.element('gemini-prompt').value;assert.match(text,goals[day]);if(mode==='mode-selfie')assert.match(text,/attached reference image.*natural skin tone/);texts.push(text);}
   assert.equal(new Set(texts).size,10);
   if(mode==='mode-scratch')for(const text of texts)all.add(text);
  }
 }
 assert.equal(all.size,40);
});

test('track selection totals and checkout reflect only selected tracks',async()=>{
 let payload;
 const h=harness((path,options)=>{if(path.includes('/checkout')){payload=JSON.parse(options.body);return ok({authorization_url:'https://checkout.paystack.com/selected'});}const p=profile();p.modules[4].state='available';return ok(p);});await settle();h.nodes[4].listeners.click();
  h.element('track-video').checked=true;h.element('track-video').listeners.change();assert.match(h.element('track-total').textContent,/8,000/);assert.match(h.element('bundle-saving').textContent,/save ₦2,000/);
 await h.element('checkout-button').listeners.click();assert.equal(payload.tracks.length,2);
 h.element('track-landing').checked=false;h.element('track-landing').listeners.change();assert.match(h.element('track-total').textContent,/5,000/);
 await h.element('checkout-button').listeners.click();assert.deepEqual(payload.tracks,['video']);
 h.element('track-video').checked=false;h.element('track-video').listeners.change();assert.equal(h.element('checkout-button').disabled,true);
});
test('owned tracks expose guides and cannot be selected for repurchase',async()=>{
 const h=harness(()=>{const p=profile();p.tier1_video=true;p.modules[4].state='unlocked';return ok(p);});await settle();h.nodes[4].listeners.click();
 assert.equal(h.element('open-video-track').hidden,false);assert.equal(h.element('open-landing-track').hidden,true);assert.equal(h.element('track-video').disabled,true);assert.equal(h.element('track-landing').disabled,false);
});


test('Gemini offers native new-tab and same-tab navigation while saving the prompt',async()=>{
 const html=fs.readFileSync(__dirname+'/dashboard.html','utf8');
 assert.match(html,/<a id="btn-open-gemini"[^>]*href="https:\/\/gemini.google.com\/"[^>]*target="_blank"/);
 assert.match(html,/<a id="gemini-same-tab"[^>]*href="https:\/\/gemini.google.com\/"[^>]*target="_self"/);
 const source=fs.readFileSync(__dirname+'/dashboard.js','utf8');assert.ok(!source.includes("window.open('https://gemini.google.com"));
 const h=harness(()=>ok(profile()));await settle();h.element('gemini-prompt').value='My phone prompt';
 h.element('btn-open-gemini').listeners.click();h.nodes[0].listeners.click();h.nodes[1].listeners.click();assert.equal(h.element('gemini-prompt').value,'My phone prompt');
});

test('Gemini link copy works with a manual fallback and leaves the prompt unchanged',async()=>{
 const h=harness(()=>ok(profile()));await settle();h.element('gemini-prompt').value='My creative prompt';
 let copied;h.context.navigator.clipboard.writeText=async value=>{copied=value;};
 await h.element('copy-gemini-link').listeners.click();assert.equal(copied,'https://gemini.google.com/');assert.match(h.element('gemini-launch-status').textContent,/copy your prompt again/);assert.equal(h.element('gemini-prompt').value,'My creative prompt');
 h.context.navigator.clipboard.writeText=async()=>{throw Error('blocked');};await h.element('copy-gemini-link').listeners.click();assert.equal(h.element('gemini-web-address').selected,true);assert.match(h.element('gemini-launch-status').textContent,/manually/);
});

test('pilot saves non-binding track interest instead of starting checkout',async()=>{
 let interest,checkout=false;
 const h=harness((path,options)=>{if(path.includes('/payments/checkout')){checkout=true;return ok({});}if(path==='/api/v1/tier-1/interest'){interest=JSON.parse(options.body);return ok({status:'interest_saved'});}const p=profile();p.pilot_mode=true;p.modules[4].state='available';p.interested_tracks=['video'];return ok(p);});await settle();h.nodes[4].listeners.click();
 assert.match(h.element('checkout-button').textContent,/Register interest/);assert.match(h.element('tier1-purchase-note').textContent,/No payment/);assert.match(h.element('tier1-interest-status').textContent,/CapCut/);
 await h.element('checkout-button').listeners.click();assert.deepEqual(interest.tracks,['landing_pages']);assert.equal(checkout,false);
});


test('Prompt Lab uses one editable lesson field and selecting a theme is enough to complete',async()=>{
 let submission;
 const h=harness((path,options)=>{if(path==='/api/v1/curriculum/complete'){submission=JSON.parse(options.body);return ok({});}return ok(profile());});await settle();
 h.element('open-prompt-lab').listeners.click();assert.equal(h.element('prompt-lab-dialog-title').focused,true);assert.equal(h.element('prompt-lab-dialog').scrollTop,0);
 const choices=h.element('studio-chips').children;choices[4].listeners.click();const expected=h.element('gemini-prompt').value;
 assert.equal(choices[4].attributes['aria-pressed'],'true');assert.equal(choices[0].attributes['aria-pressed'],'false');assert.match(h.element('selected-prompt-name').textContent,/5\./);
 await h.element('submission-form').listeners.submit({preventDefault(){}});assert.equal(submission.submission,expected);assert.equal(submission.day,2);
 const html=fs.readFileSync(__dirname+'/dashboard.html','utf8');assert.ok(!html.includes('id="reflection"'));assert.ok(!html.includes('id="submission"'));assert.equal((html.match(/<textarea/g)||[]).length,1);
});
test('empty prompt cannot complete a lesson',async()=>{
 let posts=0;const h=harness((path)=>{if(path.includes('/complete'))posts++;return ok(profile());});await settle();h.element('gemini-prompt').value='';await h.element('submission-form').listeners.submit({preventDefault(){}});assert.equal(posts,0);assert.match(h.element('notice').textContent,/Choose a prompt/);
});
test('landing and dashboard expose exact WhatsApp and email contacts',()=>{
 for(const name of ['landing','dashboard']){const html=fs.readFileSync(__dirname+'/'+name+'.html','utf8');assert.ok(html.includes('https://wa.me/2348128162561?text='));assert.ok(html.includes('mailto:eyungemmanuel@gmail.com'));assert.ok(html.includes('Contact us'));}
});

test('legacy draft text restores into the single prompt without discarding old notes',async()=>{
 const h=harness(()=>ok(profile()));await settle();const key='meuseai-draft:'+profile().full_name+':day_1';h.context.localStorage.setItem(key,JSON.stringify({submission:'My legacy exercise',reflection:'My earlier observations'}));h.nodes[0].listeners.click();assert.equal(h.element('gemini-prompt').value,'My legacy exercise');h.element('gemini-prompt').value='Updated prompt';h.element('gemini-prompt').listeners.input();const saved=JSON.parse(h.context.localStorage.getItem(key));assert.equal(saved.prompt,'Updated prompt');assert.equal(saved.reflection,'My earlier observations');
});

test('quick start opens the available lesson from a locked selection and returns focus',async()=>{
 const h=harness(()=>ok(profile()));await settle();
 h.nodes[3].listeners.click();
 assert.equal(h.element('open-prompt-lab').disabled,true);
 assert.equal(h.element('quick-start-lab').disabled,false);
 h.element('quick-start-lab').listeners.click({currentTarget:h.element('quick-start-lab')});
 assert.equal(h.element('module-title').textContent,'Module 2');
 assert.equal(h.element('prompt-lab-dialog').open,true);
 h.element('close-prompt-lab').listeners.click();
 assert.equal(h.element('quick-start-lab').focused,true);
 assert.equal(h.element('quick-start-lab').attributes['aria-expanded'],'false');
});
test('quick start revisits completed practice only when no lesson is available',async()=>{
  const p=profile();p.modules[1].state='locked';
  const h=harness(()=>ok(p));await settle();
  assert.match(h.element('quick-start-lab').textContent,/Practise again/);
  h.element('quick-start-lab').listeners.click();
  assert.equal(h.element('module-title').textContent,'Module 1');
  assert.equal(h.element('submission-form').hidden,true);
 const locked=profile();locked.modules.forEach(m=>m.state='locked');
 const blocked=harness(()=>ok(locked));await settle();
 assert.equal(blocked.element('quick-start-lab').disabled,true);
 blocked.element('quick-start-lab').listeners.click();
 assert.equal(blocked.element('prompt-lab-dialog').hidden,true);
});

test('three choices assemble live, keep wardrobe consistent and copy the current prompt', async () => {
 const h=harness(()=>ok(profile()));await settle();
 const vibes=h.element('studio-chips').children;
 const wardrobe=h.element('builder-wardrobe').children;
 const light=h.element('builder-light').children;
 assert.equal(wardrobe.length,4);assert.equal(light.length,4);
 vibes[2].listeners.click();const original=h.element('gemini-prompt').value;
 wardrobe[1].listeners.click();light[2].listeners.click();
 const assembled=h.element('gemini-prompt').value;
 assert.notEqual(assembled,original);
 assert.match(assembled,/tailored suit/);assert.match(assembled,/simple portrait studio/);assert.match(assembled,/soft, flattering studio light/);
 assert.match(assembled,/attached reference image/);
 assert.equal(wardrobe[1].attributes['aria-pressed'],'true');assert.equal(wardrobe[0].attributes['aria-pressed'],'false');
 assert.equal(light[2].attributes['aria-pressed'],'true');
 let copied;h.context.navigator.clipboard.writeText=async value=>{copied=value;};
 await h.element('btn-copy-prompt').listeners.click();assert.equal(copied,assembled);
 assert.equal(h.element('studio-copied-note').hidden,false);
 light[1].listeners.click();assert.equal(h.element('studio-copied-note').hidden,true);
 assert.match(h.element('gemini-prompt').value,/warm sunset light/);
 h.element('mode-scratch').listeners.click();assert.doesNotMatch(h.element('gemini-prompt').value,/reference image/);assert.match(h.element('gemini-prompt').value,/tailored suit/);
});

test('builder drafts restore choices, mode and custom wording without overwriting on refresh', async () => {
 const h=harness(()=>ok(profile()));await settle();
 h.element('studio-chips').children[3].listeners.click();
 h.element('builder-wardrobe').children[2].listeners.click();
 h.element('builder-light').children[3].listeners.click();
 h.element('mode-scratch').listeners.click();
 h.element('gemini-prompt').value='My carefully edited portrait';h.element('gemini-prompt').listeners.input();
 await h.element('refresh-button').listeners.click();await settle();assert.equal(h.element('gemini-prompt').value,'My carefully edited portrait');
 h.nodes[0].listeners.click();h.nodes[1].listeners.click();
 assert.equal(h.element('gemini-prompt').value,'My carefully edited portrait');
 assert.equal(h.element('mode-scratch').attributes['aria-pressed'],'true');
 assert.equal(h.element('builder-wardrobe').children[2].attributes['aria-pressed'],'true');
 assert.equal(h.element('builder-light').children[3].attributes['aria-pressed'],'true');
 h.element('builder-wardrobe').children[1].listeners.click();
 assert.match(h.element('gemini-prompt').value,/tailored suit/);assert.match(h.element('gemini-prompt').value,/pink and blue neon/);
 assert.doesNotMatch(h.element('gemini-prompt').value,/evening gown/);
});

test('invalid stored choices and unavailable storage do not break the builder',async()=>{
 const h=harness(()=>ok(profile()));await settle();
 const key='meuseai-draft:'+profile().full_name+':day_1';
 h.storage.set(key,JSON.stringify({prompt:'Preserved words',builder:{vibe:999,wardrobe:'<script>',light:'invalid'}}));
 h.nodes[0].listeners.click();assert.equal(h.element('gemini-prompt').value,'Preserved words');
 assert.equal(h.element('builder-wardrobe').children[0].attributes['aria-pressed'],'true');
 h.context.localStorage.setItem=()=>{throw Error('storage denied');};
 h.element('builder-light').children[2].listeners.click();assert.match(h.element('gemini-prompt').value,/studio light/);
 const stale=h.element('builder-wardrobe').children[1];h.nodes[2].listeners.click();const before=h.element('gemini-prompt').value;
 stale.listeners.click();assert.equal(h.element('gemini-prompt').value,before);assert.equal(h.element('gemini-studio').hidden,true);
});

test('builder has three labelled steps, native keyboard controls and touch targets',()=>{
 const html=fs.readFileSync(__dirname+'/dashboard.html','utf8');
 assert.equal((html.match(/<fieldset class="builder-step">/g)||[]).length,3);
 for(const label of ['Choose your vibe','Wardrobe &amp; styling','Lighting &amp; environment'])assert.ok(html.includes(label));
 assert.match(html,/aria-describedby="builder-edit-hint"/);
 assert.match(html,/Changing a choice replaces your edits/);
 const css=fs.readFileSync(__dirname+'/dashboard.css','utf8');
 assert.match(css,/\.builder-choice, #studio-chips \.chip-btn \{ min-height:52px/);
 assert.match(css,/\.builder-choice:focus-visible/);
 assert.match(css,/#prompt-lab-dialog \.prompt-workbench textarea \{ font-family:inherit; font-size:16px/);
});

test('checkout presents Paystack, local bank transfer and instant access trust signals',()=>{
 const html=fs.readFileSync(__dirname+'/dashboard.html','utf8');
 for(const text of ['Paystack secure checkout','local bank transfer','Instant access after payment confirmation','Pro Bundle'])assert.ok(html.includes(text));
 const css=fs.readFileSync(__dirname+'/dashboard.css','utf8');assert.match(css,/\.checkout-trust/);
});
