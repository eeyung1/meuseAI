/* Server state is authoritative. Render only with textContent, never HTML data. */
(() => {
  'use strict';
  const $ = id => document.getElementById(id);
  let snapshot = null, selected = null, stream = null, refreshTimer = null;
  let loading = false, refreshAgain = false, stopped = false, busy = false;
  let studioMode = 'selfie'; // 'selfie' | 'scratch'
  let activeCelebrationDay = null;

  const labels = { available: 'Available now', completed: 'Completed', locked: 'Complete previous lesson', scheduled: 'Opens later', unlocked: 'Unlocked' };
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
    if ($('resume-course')) $('resume-course').hidden = snapshot.engagement_status !== 'lapsed' || snapshot.unsubscribed;
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

    // XP Discount Tracker
    const discountBanner = $('xp-discount-banner');
    const discountText = $('discount-banner-text');
    if (discountBanner && discountText) {
      const xp = snapshot.xp || 0;
      const discount = snapshot.discount_percent || 0;
      const price = snapshot.tier_1_price_naira || 5000;
      if (discount > 0) {
        discountText.textContent = `🎉 ${discount}% Course Discount Unlocked (${Number(xp).toLocaleString()} XP)! Tier 1 is ₦${Number(price).toLocaleString()} instead of ₦5,000.`;
      } else {
        const needed = Math.max(0, 1000 - xp);
        discountText.textContent = `💎 ${Number(xp).toLocaleString()} XP Earned · Complete all four lessons and reach 1,000 XP (need ${needed} more) to unlock 10% OFF Tier 1 (Pay ₦4,500)!`;
      }
      discountBanner.hidden = false;
    }

    if ($('checkout-button')) {
      const price = snapshot.tier_1_price_naira || 5000;
      const discount = snapshot.discount_percent || 0;
      if (discount > 0) {
        $('checkout-button').textContent = `Unlock Tier 1 · ₦${Number(price).toLocaleString()} (${discount}% XP Discount!)`;
      } else {
        $('checkout-button').textContent = 'Continue with Tier 1 · ₦5,000';
      }
    }

    if (!selected) { selected = snapshot.modules.find(module => ['available','scheduled'].includes(module.state))?.id || snapshot.modules[0]?.id; restoreDraft(); }
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

  const lessons = {
    day_1: { goal: 'Build your first image prompt', time: '15–20 minutes', learn: 'A useful prompt names a subject, describes its setting, and specifies a visual style. Start simple so you can see what each part changes.', task: 'Generate one image. Change only the setting and generate again. Keep the subject and style the same.', question: 'What changed when you changed the setting? Which version communicates your idea better?', practice: 'Try three settings for the same subject. Compare the results without changing everything at once.', example: 'A local coffee shop, on a quiet Lagos street at sunrise, in a warm editorial photography style.' },
    day_2: { goal: 'Control light and mood', time: '15–20 minutes', learn: 'Light direction, softness and colour change the feeling of an image. Keep your subject and composition constant when comparing lighting.', task: 'Reuse yesterday’s subject. Generate a soft daylight version and a dramatic side-lit version.', question: 'How did the two lighting choices change the mood or clarity?', practice: 'Try warm sunset light and cool overcast light. Describe which suits your intended audience.', example: 'Keep the subject, setting and composition unchanged. Replace the lighting with soft window light from the left and gentle shadows.' },
    day_3: { goal: 'Keep a subject consistent', time: '20 minutes', learn: 'Describe a few stable visual anchors: colours, clothing, shape or distinctive details. Repeat those anchors, and use your earlier image as a reference if available. Exact identity preservation is not guaranteed.', task: 'Create two scenes featuring the same subject. Repeat its anchors and compare the outputs.', question: 'Which details stayed consistent? What drifted, and how would you improve your instructions?', practice: 'Try a third scene. Strengthen only the details that drifted.', example: 'The same small red delivery bicycle with a cream basket and round brass bell, first outside a bakery, then beside a park. Keep the bicycle design and colours consistent.' },
    day_4: { goal: 'Refine a final portfolio image', time: '20–25 minutes', learn: 'Editing is a conversation. State what to preserve and request one specific change at a time. Judge each result against your original goal.', task: 'Choose an earlier image. Make two follow-up edits, then select your strongest version for a business, personal or creative use.', question: 'What were your two edits, what improved, and where would you use the final image?', practice: 'Create a landscape and square variation for different placements.', example: 'Keep the subject and its identity unchanged. Simplify the background and leave clear space on the right for a headline. Then make a second edit to soften the shadows.' }
  };
  const recipes = Object.fromEntries(Object.entries(lessons).map(([id, lesson]) => [id, {
    scratch: { tag: 'Build and compare', guide: 'Edit the example to reflect your own subject. Generate in Gemini, then return to record your observations.', presets: [{ label: 'Start with today’s example', text: lesson.example }] },
    selfie: { tag: 'Optional reference photo', guide: 'In Gemini, attach a photo you have permission to use. Paste the prompt and describe what to keep. A photo is optional; you can use the creative track instead.', presets: [{ label: 'Adapt a reference photo', text: 'Using my attached reference image, preserve the subject’s recognizable features and natural appearance. ' + lesson.example }] }
  }]));
  function draftKey() { return 'meuseai-draft:' + (snapshot?.student_id || snapshot?.full_name || 'learner') + ':' + selected; }
  function saveDraft() {
    if (!selected || !snapshot) return;
    try { localStorage.setItem(draftKey(), JSON.stringify({ prompt: $('gemini-prompt')?.value || '', submission: $('submission')?.value || '', reflection: $('reflection')?.value || '' })); } catch (_) { /* Storage can be disabled. */ }
  }
  function restoreDraft() {
    try {
      const saved = JSON.parse(localStorage.getItem(draftKey()) || '{}');
      if ($('gemini-prompt')) $('gemini-prompt').value = saved.prompt || '';
      if ($('submission')) $('submission').value = saved.submission || '';
      if ($('reflection')) $('reflection').value = saved.reflection || '';
    } catch (_) { /* A missing or invalid draft must not block the lesson. */ }
  }

  function updateSocialShare(dayNum) {
    if (!dayNum || !$('social-celebration')) return;
    activeCelebrationDay = dayNum;
    const studentName = snapshot?.full_name || 'A builder';
    const origin = (typeof window !== 'undefined' && window.location?.origin) ? window.location.origin : 'https://meuseai.onrender.com';
    const shareUrl = `${origin}/share?day=${dayNum}&student=${encodeURIComponent(studentName)}`;
    const quote = `I just finished Day ${dayNum} of the meuseAI Image Accelerator using Google Gemini! 🚀 Created a custom visual exploring lighting and prompt direction. Join the free 4-day sprint: ${origin}`;
    const tweet = `Day ${dayNum} with @meuseAI & Google Gemini complete! 🎨 Engineered a custom visual exploring prompt direction. Start the free 4-day sprint:`;

    const fbBtn = $('share-facebook-btn');
    if (fbBtn) fbBtn.href = `https://www.facebook.com/sharer/sharer.php?u=${encodeURIComponent(shareUrl)}&quote=${encodeURIComponent(quote)}`;

    const twBtn = $('share-twitter-btn');
    if (twBtn) twBtn.href = `https://twitter.com/intent/tweet?text=${encodeURIComponent(tweet)}&url=${encodeURIComponent(shareUrl)}&hashtags=meuseAI,GeminiAI,AIArt`;

    const liBtn = $('share-linkedin-btn');
    if (liBtn) liBtn.href = `https://www.linkedin.com/sharing/share-offsite/?url=${encodeURIComponent(shareUrl)}`;

    const waBtn = $('share-whatsapp-btn');
    if (waBtn) waBtn.href = `https://api.whatsapp.com/send?text=${encodeURIComponent(quote + ' ' + shareUrl)}`;

    if ($('celebration-title')) $('celebration-title').textContent = `Day ${dayNum} Conquered! +250 XP Awarded`;

    // Social Share Link Status
    const existingShare = snapshot?.social_shares?.find(s => s.day === dayNum);
    const form = $('share-link-form');
    const statusEl = $('share-link-status');
    if (existingShare) {
      if (form) form.hidden = true;
      if (statusEl) {
        statusEl.hidden = false;
        if (existingShare.status === 'approved') {
          statusEl.textContent = '✦ Verified! +250 XP bonus awarded for this share.';
          statusEl.style.color = 'var(--green)';
        } else if (existingShare.status === 'rejected') {
          statusEl.textContent = 'Post review was rejected. Please make sure your post is public and resubmit.';
          statusEl.style.color = '#ff6b6b';
          if (form) form.hidden = false;
        } else {
          statusEl.textContent = '⏳ Post link submitted! Admin is reviewing it to confirm +250 XP.';
          statusEl.style.color = 'var(--yellow)';
        }
      }
    } else {
      if (form) form.hidden = false;
      if (statusEl) statusEl.hidden = true;
    }

    $('social-celebration').hidden = false;
  }

  function renderStudioForModule(module) {
    const studio = $('gemini-studio');
    if (!studio) return;
    if (module.id.startsWith('day_') && ['available','completed'].includes(module.state) && recipes[module.id]) {
      const dayData = recipes[module.id];
      const recipe = dayData[studioMode] || dayData.selfie || dayData.scratch;
      if ($('studio-recipe-tag')) $('studio-recipe-tag').textContent = recipe.tag;
      if ($('studio-guide')) $('studio-guide').textContent = recipe.guide;
      const chipsContainer = $('studio-chips');
      if (chipsContainer) {
        chipsContainer.replaceChildren();
        recipe.presets.forEach(preset => {
          const btn = document.createElement('button');
          btn.type = 'button';
          btn.className = 'chip-btn';
          btn.textContent = preset.label;
          btn.addEventListener('click', () => {
            if ($('gemini-prompt')) $('gemini-prompt').value = preset.text; saveDraft();
          });
          chipsContainer.append(btn);
        });
      }
      if ($('gemini-prompt') && !$('gemini-prompt').value) {
        $('gemini-prompt').value = recipe.presets[0].text;
      }
      studio.hidden = false;
    } else {
      studio.hidden = true;
    }
  }

  function openPractice() {
    const module = snapshot?.modules.find(item => item.id === selected);
    if (!module || !['available', 'completed'].includes(module.state) || !module.id.startsWith('day_')) return;
    const dialog = $('prompt-lab-dialog');
    if (!dialog) return;
    dialog.hidden = false;
    if (typeof dialog.showModal === 'function' && !dialog.open) dialog.showModal();
    $('open-prompt-lab')?.setAttribute('aria-expanded', 'true');
    $('gemini-prompt')?.focus();
  }
  function closePractice() {
    const dialog = $('prompt-lab-dialog');
    if (!dialog) return;
    const wasOpen = !dialog.hidden;
    if (typeof dialog.close === 'function' && dialog.open) dialog.close();
    dialog.hidden = true;
    $('open-prompt-lab')?.setAttribute('aria-expanded', 'false');
    if (wasOpen) $('open-prompt-lab')?.focus();
  }
  if ($('prompt-lab-dialog')) $('prompt-lab-dialog').hidden = true;
  $('open-prompt-lab')?.addEventListener('click', openPractice);
  $('close-prompt-lab')?.addEventListener('click', closePractice);
  $('prompt-lab-dialog')?.addEventListener('cancel', event => { event.preventDefault(); closePractice(); });
  $('prompt-lab-dialog')?.addEventListener('close', () => {
    $('prompt-lab-dialog').hidden = true;
    $('open-prompt-lab')?.setAttribute('aria-expanded', 'false');
    $('open-prompt-lab')?.focus();
  });

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

    const lesson = lessons[module.id];
    const canRead = lesson && ['available','completed'].includes(module.state);
    if ($('open-prompt-lab')) $('open-prompt-lab').disabled = !canRead;
    if ($('prompt-lab-dialog-title')) $('prompt-lab-dialog-title').textContent = `${module.title} · Gemini Prompt Lab`;
    if (!canRead && $('prompt-lab-dialog') && !$('prompt-lab-dialog').hidden) closePractice();
    if ($('lesson-content')) {
      $('lesson-content').hidden = !canRead;
      if (canRead) {
        for (const [id, value] of Object.entries({ 'lesson-goal': lesson.goal, 'lesson-time': lesson.time, 'lesson-learn': lesson.learn, 'lesson-task': lesson.task, 'reflection-question': lesson.question, 'lesson-practice': lesson.practice })) $(id).textContent = value;
      }
    }
    if ($('saved-work')) {
      $('saved-work').hidden = module.state !== 'completed';
      $('saved-work-text').textContent = module.submission || 'Your completion is saved.';
    }
    if (module.state === 'scheduled') {
      const date = new Date(module.unlocks_at);
      const remaining = Math.max(0, date - Date.now());
      $('module-help').textContent = `Next lesson opens ${date.toLocaleString()} (about ${Math.ceil(remaining / 3600000)} hours). Keep practising your completed lesson while you wait.`;
      if (!remaining) scheduleRefresh();
    }
    renderStudioForModule(module);

    // Social share rendering for completed module
    if ($('social-celebration')) {
      if (module.id.startsWith('day_') && module.state === 'completed') {
        updateSocialShare(Number(module.id.slice(4)));
      } else if (!activeCelebrationDay) {
        $('social-celebration').hidden = true;
      }
    }
  }

  document.querySelectorAll('[data-module]').forEach(button => button.addEventListener('click', () => {
    if (busy || selected === button.dataset.module) return;
    saveDraft(); closePractice(); selected = button.dataset.module; restoreDraft(); if (snapshot) render();
  }));

  $('mode-selfie')?.addEventListener('click', () => {
    studioMode = 'selfie';
    $('mode-selfie')?.classList.add('active');
    $('mode-scratch')?.classList.remove('active');
    const module = snapshot?.modules.find(item => item.id === selected);
    if (module) {
      saveDraft(); if ($('gemini-prompt')) $('gemini-prompt').value = '';
      renderStudioForModule(module);
    }
  });

  $('mode-scratch')?.addEventListener('click', () => {
    studioMode = 'scratch';
    $('mode-scratch')?.classList.add('active');
    $('mode-selfie')?.classList.remove('active');
    const module = snapshot?.modules.find(item => item.id === selected);
    if (module) {
      saveDraft(); if ($('gemini-prompt')) $('gemini-prompt').value = '';
      renderStudioForModule(module);
    }
  });

  $('submission-form').addEventListener('submit', async event => {
    event.preventDefault();
    if (busy || !snapshot) return;
    const module = snapshot.modules.find(item => item.id === selected), submission = $('submission').value.trim();
    if (!module || module.state !== 'available' || !submission) return;
    busy = true; renderModule();
    try {
      const reflection = $('reflection')?.value.trim() || '';
      if (reflection.length < 20) throw new Error('Describe what you observed in at least 20 characters.');
      const dayNum = Number(module.id.slice(4));
      await request('/api/v1/curriculum/complete', { method: 'POST', body: JSON.stringify({ day: dayNum, submission: submission + "\n\nReflection: " + reflection }) });
      try { localStorage.removeItem(draftKey()); } catch (_) {}
      $('submission').value = ''; if ($('reflection')) $('reflection').value = ''; selected = null; await refresh();
      updateSocialShare(dayNum);
    } catch (error) { notice(error.message); }
    finally { busy = false; renderModule(); }
  });

  async function copyPrompt() {
    const prompt = $('gemini-prompt')?.value.trim() || '';
    const note = $('studio-copied-note');
    if (!prompt) { if (note) { note.hidden = false; note.textContent = 'Write or select a prompt first.'; } return; }
    saveDraft();
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(prompt);
      if (note) { note.hidden = false; note.textContent = 'Prompt copied. Open Gemini, paste into its message box, attach a reference photo if needed, and generate. Return here afterwards.'; }
    } catch (_) {
      $('gemini-prompt').focus(); $('gemini-prompt').select?.();
      if (note) { note.hidden = false; note.textContent = 'Automatic copying was blocked. Select and copy the prompt manually, then open Gemini and paste it.'; }
    }
  }
  $('btn-copy-prompt')?.addEventListener('click', copyPrompt);
  $('btn-open-gemini')?.addEventListener('click', () => {
    saveDraft();
    window.open('https://gemini.google.com/app', '_blank', 'noopener,noreferrer');
  });
  for (const id of ['gemini-prompt', 'submission', 'reflection']) $(id)?.addEventListener('input', saveDraft);
  $('btn-clear-draft')?.addEventListener('click', () => {
    for (const id of ['gemini-prompt', 'submission', 'reflection']) if ($(id)) $(id).value = '';
    try { localStorage.removeItem(draftKey()); } catch (_) {}
    notice('This lesson’s local draft was cleared. Submitted work is unchanged.');
  });
  $('btn-use-prompt')?.addEventListener('click', () => {
    const prompt = $('gemini-prompt')?.value || '';
    if (prompt && $('submission')) {
      $('submission').value = prompt;
      $('submission').focus(); saveDraft();
    }
  });

  $('btn-copy-share')?.addEventListener('click', () => {
    const studentName = snapshot?.full_name || 'A builder';
    const origin = (typeof window !== 'undefined' && window.location?.origin) ? window.location.origin : 'https://meuseai.onrender.com';
    const day = activeCelebrationDay || 1;
    const text = `Day ${day} with @meuseAI & Google Gemini complete! 🎨 Engineered a custom visual exploring lighting and prompt direction. Check out the free 4-day sprint: ${origin}/share?day=${day}&student=${encodeURIComponent(studentName)}`;
    if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(text).catch(() => {});
    }
    const note = $('share-copied-note');
    if (note) { note.hidden = false; setTimeout(() => { note.hidden = true; }, 4000); }
  });

  $('share-link-form')?.addEventListener('submit', async event => {
    event.preventDefault();
    if (busy || !snapshot) return;
    const url = $('share-post-url')?.value.trim();
    const day = activeCelebrationDay;
    const statusEl = $('share-link-status');
    const submitBtn = $('btn-submit-share-link');
    if (!url || !day) return;

    if (submitBtn) submitBtn.disabled = true;
    try {
      const res = await request('/api/v1/curriculum/share-link', {
        method: 'POST',
        body: JSON.stringify({ day, share_url: url, platform: 'social' })
      });
      if (statusEl) {
        statusEl.textContent = '⏳ ' + (res.message || 'Link submitted for admin confirmation (+250 XP upon approval)!');
        statusEl.style.color = 'var(--yellow)';
        statusEl.hidden = false;
      }
      if ($('share-link-form')) $('share-link-form').hidden = true;
      await refresh();
    } catch (err) {
      if (statusEl) {
        statusEl.textContent = 'Error: ' + err.message;
        statusEl.style.color = '#ff6b6b';
        statusEl.hidden = false;
      }
    } finally {
      if (submitBtn) submitBtn.disabled = false;
    }
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

  $('resume-course')?.addEventListener('click', async () => { try { await request('/api/v1/curriculum/resume', {method:'POST',body:'{}'}); await refresh(); } catch (error) { notice(error.message); } });
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
