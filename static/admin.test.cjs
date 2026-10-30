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
    this.className = '';
  }
  set innerHTML(_) { throw new Error('Untrusted HTML rendering'); }
  addEventListener(type, fn) { this.listeners[type] = fn; }
  append(...children) { this.children.push(...children); }
  replaceChildren(...children) { this.children = children; }
}

async function settle() {
  for (let i = 0; i < 4; i++) await new Promise(resolve => setImmediate(resolve));
}

function adminHarness(handler, initialToken = '') {
  const elements = new Map();
  const calls = [];
  const storage = new Map();
  if (initialToken) storage.set('meuseai_admin_token', initialToken);

  const element = id => {
    if (!elements.has(id)) elements.set(id, new Element());
    return elements.get(id);
  };

  const context = {
    document: {
      getElementById: element,
      createElement: () => new Element(),
      addEventListener(type, fn) { if (type === 'DOMContentLoaded') fn(); },
      readyState: 'complete'
    },
    window: {
      location: { search: '' }
    },
    localStorage: {
      getItem: key => storage.get(key) || null,
      setItem: (key, val) => storage.set(key, String(val)),
      removeItem: key => storage.delete(key)
    },
    URLSearchParams,
    encodeURIComponent,
    fetch: async (path, options) => {
      calls.push({ path, options });
      return handler(path, options);
    }
  };

  const code = fs.readFileSync(__dirname + '/admin.js', 'utf8');
  vm.runInNewContext(code, context);
  return { element, calls, storage, context };
}

const ok = value => ({ ok: true, status: 200, json: async () => value });

test('shows auth card when no token is present', async () => {
  const h = adminHarness(() => ok({ shares: [] }));
  await settle();
  assert.equal(h.element('admin-auth-card').hidden, false);
  assert.equal(h.element('admin-portal-content').hidden, true);
});

test('loads verification queue when token is provided and renders stats', async () => {
  const shares = [
    { id: 1, student_id: 's1', student_name: 'Amara', student_email: 'amara@example.com', day_number: 1, share_url: 'https://facebook.com/post1', platform: 'facebook', status: 'pending', xp_awarded: 0, created_at: '2026-10-29T10:00:00Z' },
    { id: 2, student_id: 's2', student_name: 'Chioma', student_email: 'chioma@example.com', day_number: 2, share_url: 'https://instagram.com/p/2', platform: 'instagram', status: 'approved', xp_awarded: 200, created_at: '2026-10-29T10:30:00Z' }
  ];

  const h = adminHarness(path => {
    if (path.startsWith('/api/v1/admin/social-shares')) return ok({ shares });
    return ok({});
  }, 'secret-token-123');
  await settle();

  assert.equal(h.element('admin-portal-content').hidden, false);
  assert.equal(h.element('stat-pending').textContent, 1);
  assert.equal(h.element('stat-approved').textContent, 1);
  assert.equal(h.element('stat-total').textContent, 2);

  // Check table has 2 rows
  const tbody = h.element('shares-tbody');
  assert.equal(tbody.children.length, 2);
  assert.equal(tbody.children[0].children[0].children[0].textContent, 'Amara');
});

test('approving a share posts to /api/v1/admin/social-shares/review', async () => {
  let reviewPayload = null;
  const shares = [
    { id: 42, student_id: 's1', student_name: 'Fatima', student_email: 'fatima@example.com', day_number: 1, share_url: 'https://facebook.com/post', platform: 'facebook', status: 'pending', xp_awarded: 0, created_at: '2026-10-29T10:00:00Z' }
  ];

  const h = adminHarness((path, options) => {
    if (path === '/api/v1/admin/social-shares/review') {
      reviewPayload = JSON.parse(options.body);
      return ok({ share_id: 42, status: 'approved', xp_awarded: 200 });
    }
    if (path.startsWith('/api/v1/admin/social-shares')) {
      return ok({ shares });
    }
    return ok({});
  }, 'secret-token-123');
  await settle();

  // Find approve button in actions column (index 6) of first row
  const tbody = h.element('shares-tbody');
  const actionTd = tbody.children[0].children[6];
  const approveBtn = actionTd.children[0];
  await approveBtn.listeners.click();
  await settle();

  assert.deepEqual(reviewPayload, { share_id: 42, action: 'approve' });
});

test('admin sees latest pilot interest without interpreting names as HTML',async()=>{
 const h=adminHarness(path=>path==='/api/v1/admin/tier-1-interest'?ok({entries:[{name:'<img onerror=x>',email:'pilot@example.com',tracks:'landing_pages,video'}]}):ok({shares:[]}), 'secret-token-123');await settle();
 assert.equal(h.element('pilot-interest-list').children.length,1);assert.match(h.element('pilot-interest-list').children[0].textContent,/<img onerror=x>.*Lovable.*CapCut/);
});
