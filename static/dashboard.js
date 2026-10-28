/* Server state is authoritative. Render only with textContent, never HTML data. */
(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  let snapshot = null, selected = null, stream = null, refreshTimer = null;
  let loading = false, refreshAgain = false, stopped = false, busy = false;
  const labels = { available: 'Available now', completed: 'Completed', locked: 'Locked', unlocked: 'Unlocked' };
  function notice(message) { $('notice').hidden = !message; $('notice').textContent = message || ''; }
  function connection(text, live = false) { $('connection').textContent = text; $('connection').parentElement.classList.toggle('live', live); }
  async function request(path, options = {}) {
    const response = await fetch(path, { credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, ...options });
    if (response.status === 401) {
      stopped = true; stream?.close(); clearTimeout(refreshTimer);
      throw new Error('Your session has expired. Please sign in again.');
    }
    if (!response.ok) throw new Error(response.status === 403 ? 'This module is not available yet. Refresh your progress and try again.' : 'We couldn’t save or load your progress. Please try again.');
    return response.json();
  }
  function scheduleRefresh() { if (!stopped) { clearTimeout(refreshTimer); refreshTimer = setTimeout(refresh, 300); } }
  async function refresh() {
    if (stopped) return;
    if (loading) { refreshAgain = true; return; }
    loading = true;
    try {
      snapshot = await request('/api/v1/dashboard/me');
      render(); notice(''); if (!stream && !stopped) connect();
    } catch (error) { notice(error.message); connection(stopped ? 'Sign in required' : 'Reconnecting'); }
    finally { loading = false; if (refreshAgain) { refreshAgain = false; scheduleRefresh(); } }
  }
  function render() {
    $('student-name').textContent = snapshot.full_name;
    $('greeting').textContent = `Welcome back, ${snapshot.full_name.split(' ')[0] || 'learner'}.`;
    $('avatar').textContent = snapshot.full_name.trim().slice(0, 1).toUpperCase() || '✦';
    $('student-tier').textContent = ({ free_week: 'Free creative foundations', tier_1_active: 'Tier 1 · Landing pages', tier_2_active: 'Tier 2 · AI coding agents', free_tier_lapsed: 'Free course paused' })[snapshot.funnel_tier] || 'Your learning journey';
    $('xp').textContent = Number(snapshot.xp).toLocaleString();
    $('streak').textContent = snapshot.streak_days;
    $('rank').textContent = snapshot.rank ? `Your cohort rank: #${snapshot.rank}` : 'Verify your email to join the cohort.';
    const days = snapshot.modules.filter(module => module.id.startsWith('day_'));
    const done = days.filter(module => module.state === 'completed').length;
    $('completed-count').textContent = `${done} / 4 days`;
    $('progress-percent').textContent = `${done * 25}%`;
    $('progress-ring').style.strokeDashoffset = String(100 - done * 25);
    if (!selected) selected = snapshot.modules.find(module => module.state === 'available')?.id || snapshot.modules[0]?.id;
    document.querySelectorAll('[data-module]').forEach(button => {
      const module = snapshot.modules.find(item => item.id === button.dataset.module);
      button.className = `node node-${snapshot.modules.indexOf(module) + 1} ${module?.state || 'locked'}`;
      button.setAttribute('aria-label', `${module?.title || 'Module'}: ${labels[module?.state] || 'Locked'}`);
      button.setAttribute('aria-pressed', String(selected === module?.id));
    });
    $('leaderboard').replaceChildren();
    for (const peer of snapshot.leaderboard) {
      const li = document.createElement('li'), name = document.createElement('span'), xp = document.createElement('span');
      li.classList.toggle('you', peer.you); name.textContent = `${peer.rank}. ${peer.label}`; xp.textContent = `${Number(peer.xp).toLocaleString()} XP`;
      li.append(name, xp); $('leaderboard').append(li);
    }
    $('activity-list').replaceChildren();
    for (const activity of snapshot.activity) {
      const li = document.createElement('li'), title = document.createElement('span'), time = document.createElement('time');
      title.textContent = activity.title; time.dateTime = activity.at;
      time.textContent = new Date(activity.at).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
      li.append(title, time); $('activity-list').append(li);
    }
    if (!snapshot.activity.length) { const li = document.createElement('li'); li.textContent = 'Your next step starts a new story. Complete an exercise to see it here.'; $('activity-list').append(li); }
    if (!snapshot.leaderboard.length) { const li = document.createElement('li'); li.textContent = 'Your cohort is getting started.'; $('leaderboard').append(li); }
    renderModule();
  }
  function renderModule() {
    if (!snapshot) return;
    const module = snapshot.modules.find(item => item.id === selected);
    if (!module) return;
    $('module-title').textContent = module.title; $('module-description').textContent = module.description;
    $('module-state').textContent = labels[module.state] || module.state; $('module-state').className = `pill ${module.state}`;
    $('module-prerequisite').textContent = module.prerequisite;
    $('module-xp').textContent = module.xp ? `+${module.xp} XP` : 'Tier 1';
    $('submission-form').hidden = !module.id.startsWith('day_') || module.state !== 'available';
    $('checkout-button').hidden = module.id !== 'tier_1' || module.state !== 'available';
    $('module-help').textContent = module.state === 'locked' ? module.prerequisite : module.state === 'completed' ? 'Well done. Your progress has been saved.' : module.state === 'unlocked' ? 'Your Tier 1 access is active.' : 'Take your time. Your progress will be saved when the server confirms it.';
    $('submit-button').disabled = busy || stopped; $('checkout-button').disabled = busy || stopped;
  }
  document.querySelectorAll('[data-module]').forEach(button => button.addEventListener('click', () => {
    if (busy || selected === button.dataset.module) return;
    selected = button.dataset.module; $('submission').value = ''; if (snapshot) render();
  }));
  $('submission-form').addEventListener('submit', async event => {
    event.preventDefault();
    if (busy || !snapshot) return;
    const module = snapshot.modules.find(item => item.id === selected), submission = $('submission').value.trim();
    if (!module || module.state !== 'available' || !submission) return;
    busy = true; renderModule();
    try {
      await request('/api/v1/curriculum/complete', { method: 'POST', body: JSON.stringify({ day: Number(module.id.slice(4)), submission }) });
      $('submission').value = ''; selected = null; await refresh();
    } catch (error) { notice(error.message); }
    finally { busy = false; renderModule(); }
  });
  $('checkout-button').addEventListener('click', async () => {
    if (busy) return;
    busy = true; renderModule();
    try {
      const result = await request('/api/v1/payments/checkout', { method: 'POST', body: JSON.stringify({ tier: 'tier_1' }) });
      const target = new URL(result.authorization_url);
      if (target.protocol !== 'https:' || target.host !== 'checkout.paystack.com' || target.username || target.password) throw new Error('Checkout is unavailable. Please try again.');
      location.assign(target.href);
    } catch (error) { notice(error.message); }
    finally { busy = false; renderModule(); }
  });
  $('refresh-button').addEventListener('click', refresh);
  function connect() {
    if (stopped || stream) return;
    stream = new EventSource('/api/v1/telemetry/stream');
    stream.onopen = () => { connection('Live updates', true); refresh(); };
    stream.onerror = () => { if (!stopped) connection('Reconnecting'); };
    for (const type of ['activity', 'progress', 'rank', 'announcement']) stream.addEventListener(type, scheduleRefresh);
  }
  let fallback = setInterval(() => { if (!document.hidden) refresh(); }, 30000);
  document.addEventListener('visibilitychange', () => { if (!document.hidden) refresh(); });
  window.addEventListener('pagehide', () => { stream?.close(); stream = null; clearTimeout(refreshTimer); clearInterval(fallback); });
  window.addEventListener('pageshow', event => { if (event.persisted && !stopped) { fallback = setInterval(() => { if (!document.hidden) refresh(); }, 30000); refresh(); } });
  // Authenticate through REST first so a 401 cannot trigger an endless SSE loop.
  refresh().then(() => { if (snapshot && !stopped) connect(); });
})();
