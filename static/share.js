(() => {
  'use strict';
  const params = new URLSearchParams(window.location.search);
  const student = params.get('student') ? params.get('student').trim() : '';
  const day = params.get('day') ? params.get('day').trim() : '';

  const dayTitles = {
    '1': 'Prompt Architecture & Lens Direction',
    '2': 'Volumetric Lighting & Texture Physics',
    '3': 'Character Consistency & Style Anchors',
    '4': 'The Elite Showcase & Commercial Capstone'
  };

  const heading = document.getElementById('share-heading');
  const body = document.getElementById('share-body');
  const eyebrow = document.getElementById('badge-eyebrow');

  const safeDay = ['1', '2', '3', '4'].includes(day) ? day : '1';
  const displayName = student || 'meuseAI learner';
  const canvas = document.getElementById('share-badge-canvas');
  const ctx = canvas?.getContext?.('2d');
  if (ctx) {
    const width = canvas.width, height = canvas.height;
    const gradient = ctx.createLinearGradient?.(0, 0, width, height);
    if (gradient?.addColorStop) { gradient.addColorStop(0, '#07132d'); gradient.addColorStop(1, '#2b1f05'); ctx.fillStyle = gradient; }
    else ctx.fillStyle = '#07132d';
    ctx.fillRect(0, 0, width, height);
    ctx.fillStyle = '#ffd60a'; ctx.fillRect(0, 0, width, 8); ctx.fillRect(0, height - 8, width, 8);
    ctx.textAlign = 'center'; ctx.fillStyle = '#ffd60a'; ctx.font = '800 18px system-ui'; ctx.fillText('meuseAI', width / 2, 58);
    ctx.fillStyle = '#f8fafc'; ctx.font = '800 30px system-ui'; ctx.fillText(`DAY ${safeDay}`, width / 2, 145); ctx.fillText('COMPLETE', width / 2, 185);
    ctx.fillStyle = '#57d9a3'; ctx.font = '800 18px system-ui'; ctx.fillText('+250 XP SHARE BADGE', width / 2, 250);
    ctx.fillStyle = '#f8fafc'; ctx.font = '700 15px system-ui'; ctx.fillText(displayName.slice(0, 28), width / 2, 320);
    ctx.fillStyle = '#a5b2cc'; ctx.font = '500 10px system-ui'; ctx.fillText('meuseai.onrender.com', width / 2, 420);
  }
  document.getElementById('download-public-badge')?.addEventListener('click', () => {
    if (!canvas?.toDataURL) return;
    const link = document.createElement('a'); link.href = canvas.toDataURL('image/png'); link.download = `meuseAI-day-${safeDay}-story.png`; link.click();
  });

  if (student && day) {
    const title = dayTitles[day] || `Day ${day} Exercise`;
    if (heading) heading.textContent = `${student} mastered Day ${day} on meuseAI!`;
    if (body) body.textContent = `${student} built a custom visual with Google Gemini for "${title}" and unlocked a shareable creator milestone.`;
    if (eyebrow) eyebrow.textContent = `DAY ${day} CONQUERED · ${student.toUpperCase()}`;
    document.title = `${student} completed Day ${day} · meuseAI`;
  } else if (day) {
    const title = dayTitles[day] || `Day ${day}`;
    if (heading) heading.textContent = `Day ${day} Mastered on meuseAI!`;
    if (body) body.textContent = `A learner built a custom visual with Google Gemini for "${title}" and unlocked a shareable creator milestone.`;
    if (eyebrow) eyebrow.textContent = `DAY ${day} COMPLETED`;
  }
})();
