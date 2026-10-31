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
    this.style = {};
    this.hidden = false;
    this.disabled = false;
    this.value = '';
    this.checked = false;
    this.className = '';
    this.classList = {
      add: cls => { if (!this.className.includes(cls)) this.className += ' ' + cls; },
      remove: cls => { this.className = this.className.replace(cls, '').trim(); },
      contains: cls => this.className.includes(cls)
    };
  }
  set innerHTML(_) { throw new Error('Untrusted HTML rendering'); }
  addEventListener(type, fn) { this.listeners[type] = fn; }
  setAttribute(name, value) { this.attributes[name] = value; }
  getAttribute(name) { return this.attributes[name]; }
  focus() { this.focused = true; }
  reset() { this.value = ''; }
}

async function settle() {
  for (let i = 0; i < 4; i++) await new Promise(resolve => setImmediate(resolve));
}

function harness(handler, search = '') {
  const elements = new Map();
  const calls = [];
  const element = id => {
    if (!elements.has(id)) elements.set(id, new Element());
    return elements.get(id);
  };
  const docListeners = {};
  const context = {
    document: {
      getElementById: element,
      addEventListener(type, fn) { docListeners[type] = fn; }
    },
    window: {
      location: { search, pathname: '/' }
    },
    URLSearchParams,
    fetch: async (path, options) => {
      calls.push({ path, options });
      return handler(path, options);
    }
  };

  vm.runInNewContext(fs.readFileSync(__dirname + '/landing.js', 'utf8'), context);
  if (docListeners['DOMContentLoaded']) {
    docListeners['DOMContentLoaded']();
  }
  return { element, calls, docListeners };
}

test('tab switching toggles signup and login panels', async () => {
  const h = harness(() => ({ ok: true, status: 200, json: async () => ({}) }));
  const signupBtn = h.element('tab-signup');
  const loginBtn = h.element('tab-login');
  const signupPanel = h.element('panel-signup');
  const loginPanel = h.element('panel-login');

  assert.equal(signupBtn.classList.contains('active'), true);
  assert.equal(loginPanel.hidden, true); // initially hidden before click

  // Click login tab
  loginBtn.listeners['click']();
  assert.equal(loginBtn.classList.contains('active'), true);
  assert.equal(signupBtn.classList.contains('active'), false);
  assert.equal(signupPanel.hidden, true);
  assert.equal(loginPanel.hidden, false);

  // Click signup tab back
  signupBtn.listeners['click']();
  assert.equal(signupBtn.classList.contains('active'), true);
  assert.equal(loginBtn.classList.contains('active'), false);
  assert.equal(signupPanel.hidden, false);
  assert.equal(loginPanel.hidden, true);
});

test('URL parameter ?tab=login automatically activates login tab', async () => {
  const h = harness(() => ({ ok: true, status: 200, json: async () => ({}) }), '?tab=login');
  assert.equal(h.element('tab-login').classList.contains('active'), true);
  assert.equal(h.element('panel-signup').hidden, true);
  assert.equal(h.element('panel-login').hidden, false);
});

test('signup form validates input and submits payload', async () => {
  let submittedPayload = null;
  const h = harness((path, options) => {
    if (path === '/api/v1/students/signup') {
      submittedPayload = JSON.parse(options.body);
      return { ok: true, status: 202, json: async () => ({ message: 'Verification email sent' }) };
    }
    return { ok: false, status: 400 };
  });

  const form = h.element('signup-form');
  const nameInput = h.element('signup-name');
  const emailInput = h.element('signup-email');
  const consentBox = h.element('signup-consent');
  const feedback = h.element('form-feedback');

  // Submit empty
  form.listeners['submit']({ preventDefault() {} });
  assert.match(feedback.textContent, /enter your full name/i);

  // Submit without email
  nameInput.value = 'Ada Lovelace';
  form.listeners['submit']({ preventDefault() {} });
  assert.match(feedback.textContent, /valid email/i);

  // Submit without consent
  emailInput.value = 'ada@example.com';
  consentBox.checked = false;
  form.listeners['submit']({ preventDefault() {} });
  assert.match(feedback.textContent, /consent/i);

  // Valid submit
  consentBox.checked = true;
  form.listeners['submit']({ preventDefault() {} });
  await settle();

  assert.equal(submittedPayload.full_name, 'Ada Lovelace');
  assert.equal(submittedPayload.email, 'ada@example.com');
  assert.equal(submittedPayload.consent, true);
  assert.match(feedback.textContent, /Verification email sent/i);
  assert.equal(feedback.className.includes('success'), true);
});

test('magic link login submits email to /api/v1/students/magic-link', async () => {
  let requestedEmail = null;
  const h = harness((path, options) => {
    if (path === '/api/v1/students/magic-link') {
      requestedEmail = JSON.parse(options.body).email;
      return { ok: true, status: 202, json: async () => ({}) };
    }
    return { ok: false, status: 400 };
  });

  const form = h.element('login-form');
  const emailInput = h.element('login-email');
  const feedback = h.element('form-feedback');

  emailInput.value = 'ada@example.com';
  form.listeners['submit']({ preventDefault() {} });
  await settle();

  assert.equal(requestedEmail, 'ada@example.com');
  assert.match(feedback.textContent, /Sign-in link sent/i);
});

test('resend verification trigger posts to /api/v1/students/resend-verification', async () => {
  let resendEmail = null;
  const h = harness((path, options) => {
    if (path === '/api/v1/students/resend-verification') {
      resendEmail = JSON.parse(options.body).email;
      return { ok: true, status: 202, json: async () => ({}) };
    }
    return { ok: false, status: 400 };
  });

  const emailInput = h.element('login-email');
  const resendBtn = h.element('trigger-resend');
  const feedback = h.element('form-feedback');

  emailInput.value = 'ada@example.com';
  resendBtn.listeners['click']();
  await settle();

  assert.equal(resendEmail, 'ada@example.com');
  assert.match(feedback.textContent, /fresh verification link/i);
});

test('landing page explains additive track pricing and previews both selections',()=>{
 const h=harness(()=>({ok:true,status:200,json:async()=>({})}));
 h.element('preview-landing').checked=true;h.element('preview-video').checked=true;h.element('preview-video').listeners.change();assert.match(h.element('preview-track-total').textContent,/10,000.*9,000/);
 h.element('preview-landing').checked=false;h.element('preview-landing').listeners.change();assert.match(h.element('preview-track-total').textContent,/5,000.*4,500/);
 const html=fs.readFileSync(__dirname+'/landing.html','utf8');assert.match(html,/only the tracks you purchase unlock/i);assert.match(html,/credits.*separate/);
});

test('landing initialization leaves the phone keyboard closed until the learner chooses a field',()=>{
 const h=harness(()=>({ok:true,status:200,json:async()=>({})}));
 assert.notEqual(h.element('signup-name').focused,true);
 h.element('tab-login').listeners.click();
 assert.equal(h.element('login-email').focused,true);
});

test('public course narrative and lookbook stay visible without JavaScript', () => {
  const html = fs.readFileSync(__dirname + '/landing.html', 'utf8');
  assert.doesNotMatch(html, /<details\b|<summary\b/i);
  for (const id of ['lookbook', 'join', 'how-it-works', 'curriculum', 'journey', 'faq', 'contact']) {
    assert.match(html, new RegExp(`id="${id}"`));
  }
  assert.equal((html.match(/class="lookbook-card"/g) || []).length, 4);
  assert.equal((html.match(/role="img" aria-label="AI-generated [^"]+"/g) || []).length, 4);
  assert.match(html, /AI-generated inspiration, not student submissions/);
  assert.match(html, /Paid enrollment is currently paused/);
  assert.match(html, /at least 24 hours later/);
  assert.match(html, /href="#join">Join the free course/);
  const css = fs.readFileSync(__dirname + '/landing.css', 'utf8');
  assert.equal((css.match(/data:image\/webp;base64,/g) || []).length, 1);
  assert.doesNotMatch(css, /url\(["']?https?:/);
});

test('track preview handles none, one and both without starting payment', () => {
  const h = harness(() => { throw new Error('Preview must not make a network request'); });
  const landing = h.element('preview-landing');
  const video = h.element('preview-video');
  assert.match(h.element('preview-track-total').textContent, /Select one or both/);
  landing.checked = true;
  landing.listeners.change();
  assert.match(h.element('preview-track-total').textContent, /1 track: ₦5,000/);
  assert.match(h.element('preview-track-total').textContent, /₦4,500/);
  video.checked = true;
  video.listeners.change();
  assert.match(h.element('preview-track-total').textContent, /2 tracks: ₦10,000/);
  assert.match(h.element('preview-track-total').textContent, /₦9,000/);
  assert.equal(h.calls.length, 0);
});
