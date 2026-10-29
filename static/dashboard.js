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
  const recipes = {
    day_1: {
      tag: 'Lens & Perspective',
      guide: 'Formula: [Subject] + [Setting] + [Camera/Lens] + [Lighting Mood] + [Aspect Ratio]',
      presets: [
        { label: 'Cinematic Portrait (85mm)', text: 'Cinematic photograph of a cybernetic warrior wearing weathered brass armor, standing in a rain-slicked neon street, 85mm portrait lens, f/1.4 shallow depth of field, neon rim lighting reflecting off wet asphalt, 8k resolution, ultra-detailed --ar 16:9' },
        { label: 'Atmospheric Wide (24mm)', text: 'Atmospheric environmental wide shot of an Afrofuturist observatory on a misty cliffside at dawn, 24mm wide angle lens, soft volumetric golden hour beams, cinematic composition --ar 16:9' },
        { label: 'Dynamic Action Shot', text: 'Dynamic street-level action photograph of a high-speed hovercraft racing through a bustling night market, low angle camera perspective, motion blur in background, sharp subject clarity, cinematic --ar 21:9' }
      ]
    },
    day_2: {
      tag: 'Lighting Physics',
      guide: 'Formula: Macro/Subject + Volumetric Lighting + Caustics/Refraction + Material Textures',
      presets: [
        { label: 'Crystal & Water Caustics', text: 'Macro close-up photography of a translucent crystal orchid with morning dew drops, dramatic volumetric golden-hour backlighting, optical caustic refraction through water droplets, subsurface scattering, extreme tactile texture, dark moody bokeh background --ar 1:1' },
        { label: 'Obsidian & Molten Gold', text: 'Sculptural macro shot of cracked black obsidian stone infused with glowing veins of molten liquid gold, intense specular highlights, volumetric smoke wisps, high contrast dramatic studio lighting --ar 16:9' },
        { label: 'Neon Bioluminescence', text: 'Deep sea bioluminescent creature glowing with cyan and violet light pulses, soft volumetric underwater rays, ultra-fine microscopic surface textures, pitch-black ocean backdrop --ar 1:1' }
      ]
    },
    day_3: {
      tag: 'Character Anchor',
      guide: 'Formula: Character Archetype + Unique Visual Anchor + Wardrobe Lock + Perspective Set',
      presets: [
        { label: 'Cyber Pilot with Gold Visor', text: 'Character concept portrait of Ace Pilot Maya, signature visual anchor: glowing hexagonal gold flight visor covering left eye, midnight-blue flight suit with orange warning stripes, studio key lighting, character consistency sheet --ar 16:9' },
        { label: 'Nomadic Alchemist', text: 'Character portrait of an elderly desert alchemist, signature visual anchor: intricate silver spiral earrings and emerald-embroidered silk scarf, weathered sun-drenched facial textures, cinematic 50mm film still --ar 16:9' },
        { label: 'Architect of Tomorrow', text: 'Studio editorial portrait of an avant-garde architect, signature visual anchor: asymmetric geometric titanium eyeglasses, structured charcoal wool coat, clean minimalist white studio lighting --ar 16:9' }
      ]
    },
    day_4: {
      tag: 'Capstone Showcase',
      guide: 'Formula: Hero Commercial Concept + Studio Lighting + Color Grading + Landing Page Ready',
      presets: [
        { label: 'Autonomous AI Neural Audio', text: 'Commercial hero visual of an autonomous AI neural headset made of matte titanium and frosted sea glass, floating above a minimalist black basalt pedestal, soft commercial studio diffusion, high-end editorial lighting, Hasselblad H6D clarity, landing page hero banner --ar 16:9' },
        { label: 'Next-Gen Smart Footwear', text: 'Commercial editorial photograph of a futuristic aerodynamic sneaker made with bioluminescent mesh, dynamic floating angle, commercial advertising studio lighting, clean luxury gradient background --ar 16:9' },
        { label: 'Eco-Living Smart Pod', text: 'Architectural hero exterior of a sustainable solar glass residential pod nestled in Scandinavian pine woods, dusk lighting, warm interior glow, architectural digest quality --ar 16:9' }
      ]
    }
  };

  let activeCelebrationDay = null;
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
    $('social-celebration').hidden = false;
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

    // Gemini Studio rendering
    const studio = $('gemini-studio');
    if (studio) {
      if (module.id.startsWith('day_') && module.state === 'available' && recipes[module.id]) {
        const recipe = recipes[module.id];
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
              if ($('gemini-prompt')) $('gemini-prompt').value = preset.text;
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
    selected = button.dataset.module; $('submission').value = ''; if (snapshot) render();
  }));
  $('submission-form').addEventListener('submit', async event => {
    event.preventDefault();
    if (busy || !snapshot) return;
    const module = snapshot.modules.find(item => item.id === selected), submission = $('submission').value.trim();
    if (!module || module.state !== 'available' || !submission) return;
    busy = true; renderModule();
    try {
      const dayNum = Number(module.id.slice(4));
      await request('/api/v1/curriculum/complete', { method: 'POST', body: JSON.stringify({ day: dayNum, submission }) });
      $('submission').value = ''; selected = null; await refresh();
      updateSocialShare(dayNum);
    } catch (error) { notice(error.message); }
    finally { busy = false; renderModule(); }
  });

  $('btn-open-gemini')?.addEventListener('click', () => {
    const prompt = $('gemini-prompt')?.value || '';
    if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(prompt).catch(() => {});
    }
    if (typeof window !== 'undefined' && window.open) {
      window.open('https://gemini.google.com/app', '_blank', 'noopener,noreferrer');
    }
    const note = $('studio-copied-note');
    if (note) { note.hidden = false; setTimeout(() => { note.hidden = true; }, 4000); }
  });

  $('btn-use-prompt')?.addEventListener('click', () => {
    const prompt = $('gemini-prompt')?.value || '';
    if (prompt && $('submission')) {
      $('submission').value = prompt;
      $('submission').focus();
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
