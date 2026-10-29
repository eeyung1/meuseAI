const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

class Element {
  constructor() {
    this.textContent = '';
  }
}

function runShareScript(search = '') {
  const elements = new Map();
  const getElementById = id => {
    if (!elements.has(id)) elements.set(id, new Element());
    return elements.get(id);
  };
  const doc = {
    title: '',
    getElementById
  };
  const win = {
    location: { search }
  };
  const context = {
    document: doc,
    window: win,
    URLSearchParams,
    decodeURIComponent
  };
  const code = fs.readFileSync(__dirname + '/share.js', 'utf8');
  vm.runInNewContext(code, context);
  return { elements, doc };
}

test('share page populates customized student achievement for day 1', () => {
  const { elements, doc } = runShareScript('?day=1&student=Alex%20Morgan');
  const heading = elements.get('share-heading');
  const body = elements.get('share-body');
  const eyebrow = elements.get('badge-eyebrow');

  assert.equal(heading.textContent, 'Alex Morgan mastered Day 1 on meuseAI!');
  assert.match(body.textContent, /Prompt Architecture & Lens Direction/);
  assert.match(body.textContent, /250 XP/);
  assert.equal(eyebrow.textContent, 'DAY 1 CONQUERED · ALEX MORGAN');
  assert.equal(doc.title, 'Alex Morgan completed Day 1 · meuseAI');
});

test('share page handles day parameter without student name', () => {
  const { elements } = runShareScript('?day=2');
  const heading = elements.get('share-heading');
  const body = elements.get('share-body');

  assert.equal(heading.textContent, 'Day 2 Mastered on meuseAI!');
  assert.match(body.textContent, /Volumetric Lighting & Texture Physics/);
});

