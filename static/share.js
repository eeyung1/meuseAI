(() => {
  'use strict';
  const params = new URLSearchParams(window.location.search);
  const student = params.get('student') ? decodeURIComponent(params.get('student')).trim() : '';
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

  if (student && day) {
    const title = dayTitles[day] || `Day ${day} Exercise`;
    if (heading) heading.textContent = `${student} mastered Day ${day} on meuseAI!`;
    if (body) body.textContent = `${student} just engineered a custom visual with Google Gemini for "${title}" and earned 250 XP toward their AI Creator certification.`;
    if (eyebrow) eyebrow.textContent = `DAY ${day} CONQUERED · ${student.toUpperCase()}`;
    document.title = `${student} completed Day ${day} · meuseAI`;
  } else if (day) {
    const title = dayTitles[day] || `Day ${day}`;
    if (heading) heading.textContent = `Day ${day} Mastered on meuseAI!`;
    if (body) body.textContent = `A learner just engineered a custom visual with Google Gemini for "${title}" and earned 250 XP.`;
    if (eyebrow) eyebrow.textContent = `DAY ${day} COMPLETED`;
  }
})();

