(() => {
  'use strict';

  const STORAGE_KEY = 'meuseai_admin_token';
  const $ = id => document.getElementById(id);

  let currentToken = '';
  let showcaseLoaded=false;
  const showcase=window.MeuseAdminShowcase?.create({document,fetch,getToken:()=>currentToken,clipboard:navigator.clipboard,confirm:message=>window.confirm(message),URL:window.URL,onUnauthorized:()=>{currentToken='';localStorage.removeItem(STORAGE_KEY);showcaseLoaded=false;showAuth();}});

  function init() {
    const params = new URLSearchParams(window.location.search);
    const urlToken = params.get('admin_token');
    const storedToken = localStorage.getItem(STORAGE_KEY);
    currentToken = urlToken || storedToken || '';

    if (currentToken) {
      localStorage.setItem(STORAGE_KEY, currentToken);
      loadShares();
    } else {
      showAuth();
    }

    $('admin-auth-form')?.addEventListener('submit', e => {
      e.preventDefault();
      const token = $('admin-token-input')?.value.trim();
      if (!token) return;
      currentToken = token;
      localStorage.setItem(STORAGE_KEY, token);
      loadShares();
    });

    $('admin-logout-btn')?.addEventListener('click', () => {
      localStorage.removeItem(STORAGE_KEY);
      currentToken = '';
      showAuth();
    });

    $('filter-status')?.addEventListener('change', () => loadShares());
    $('btn-refresh-shares')?.addEventListener('click', () => loadShares());

    $('tier2-toggle-form')?.addEventListener('submit', async e => {
      e.preventDefault();
      const studentId = $('tier2-student-id')?.value.trim();
      const ready = $('tier2-ready-select')?.value === 'true';
      const note = $('tier2-result-note');
      if (!studentId || !currentToken) return;

      try {
        const res = await fetch('/api/v1/admin/toggle-tier-2', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': 'Bearer ' + currentToken
          },
          body: JSON.stringify({ student_id: studentId, tier_2_ready: ready })
        });
        const data = await res.json();
        if (!res.ok) throw new Error(data.error || 'Failed to update readiness');
        if (note) {
          note.textContent = `Success: Tier 2 readiness set to ${ready} for ${studentId}`;
          note.style.color = 'var(--green)';
          note.hidden = false;
        }
      } catch (err) {
        if (note) {
          note.textContent = 'Error: ' + err.message;
          note.style.color = '#ff6b6b';
          note.hidden = false;
        }
      }
    });
  }

  function showAuth() {
    showcase?.clear();showcaseLoaded=false;
    if ($('admin-auth-card')) $('admin-auth-card').hidden = false;
    if ($('admin-portal-content')) $('admin-portal-content').hidden = true;
    if ($('admin-logout-btn')) $('admin-logout-btn').hidden = true;
  }

  function showPortal() {
    if(!showcaseLoaded){showcaseLoaded=true;showcase?.load();}
    if ($('admin-auth-card')) $('admin-auth-card').hidden = true;
    if ($('admin-portal-content')) $('admin-portal-content').hidden = false;
    if ($('admin-logout-btn')) $('admin-logout-btn').hidden = false;
  }

  async function loadShares() {
    const filter = $('filter-status')?.value || '';
    const errEl = $('admin-auth-error');
    if (errEl) errEl.hidden = true;

    try {
      let url = '/api/v1/admin/social-shares';
      if (filter) url += '?status=' + encodeURIComponent(filter);

      const res = await fetch(url, {
        headers: { 'Authorization': 'Bearer ' + currentToken }
      });
      if (res.status === 401) {
        localStorage.removeItem(STORAGE_KEY);
        currentToken = '';
        if (errEl) {
          errEl.textContent = 'Invalid or expired admin token. Please check MEUSEAI_ADMIN_TOKEN.';
          errEl.hidden = false;
        }
        showAuth();
        return;
      }
      if (!res.ok) throw new Error('Failed to load verification queue');

      const data = await res.json();
      showPortal();
      renderShares(data.shares || []);
    } catch (err) {
      if (errEl) {
        errEl.textContent = err.message;
        errEl.hidden = false;
      }
    }
  }

  function renderShares(shares) {
    const tbody = $('shares-tbody');
    if (!tbody) return;
    tbody.replaceChildren();

    let pendingCount = 0;
    let approvedCount = 0;

    shares.forEach(s => {
      if (s.status === 'pending') pendingCount++;
      if (s.status === 'approved') approvedCount++;
    });

    if ($('stat-pending')) $('stat-pending').textContent = pendingCount;
    if ($('stat-approved')) $('stat-approved').textContent = approvedCount;
    if ($('stat-total')) $('stat-total').textContent = shares.length;

    if (shares.length === 0) {
      const tr = document.createElement('tr');
      const td = document.createElement('td');
      td.colSpan = 7;
      td.style.padding = '36px';
      td.style.textAlign = 'center';
      td.style.color = 'var(--muted)';
      td.textContent = 'No submissions found matching this filter.';
      tr.append(td);
      tbody.append(tr);
      return;
    }

    shares.forEach(item => {
      const tr = document.createElement('tr');
      tr.style.borderBottom = '1px solid var(--border)';

      // Student info
      const tdStudent = document.createElement('td');
      tdStudent.style.padding = '14px 18px';
      const nameDiv = document.createElement('strong');
      nameDiv.textContent = item.student_name || 'Student';
      nameDiv.style.display = 'block';
      const emailDiv = document.createElement('span');
      emailDiv.className = 'muted';
      emailDiv.style.fontSize = '12px';
      emailDiv.textContent = item.student_email || item.student_id;
      tdStudent.append(nameDiv, emailDiv);

      // Lesson
      const tdLesson = document.createElement('td');
      tdLesson.style.padding = '14px 18px';
      tdLesson.textContent = `Day ${item.day_number}`;

      // Platform
      const tdPlatform = document.createElement('td');
      tdPlatform.style.padding = '14px 18px';
      const platBadge = document.createElement('span');
      platBadge.style.textTransform = 'capitalize';
      platBadge.style.fontSize = '12px';
      platBadge.style.padding = '3px 8px';
      platBadge.style.borderRadius = '6px';
      platBadge.style.background = 'var(--elevated)';
      platBadge.style.border = '1px solid var(--border)';
      platBadge.textContent = item.platform || 'social';
      tdPlatform.append(platBadge);

      // Post Link
      const tdLink = document.createElement('td');
      tdLink.style.padding = '14px 18px';
      const a = document.createElement('a');
      a.href = item.share_url;
      a.target = '_blank';
      a.rel = 'noopener noreferrer';
      a.style.color = 'var(--yellow)';
      a.style.textDecoration = 'underline';
      a.textContent = 'Open Post ↗';
      tdLink.append(a);

      // Date
      const tdDate = document.createElement('td');
      tdDate.style.padding = '14px 18px';
      tdDate.className = 'muted';
      tdDate.style.fontSize = '12px';
      try {
        tdDate.textContent = new Date(item.created_at).toLocaleString();
      } catch (_) {
        tdDate.textContent = item.created_at;
      }

      // Status
      const tdStatus = document.createElement('td');
      tdStatus.style.padding = '14px 18px';
      const statusBadge = document.createElement('span');
      statusBadge.style.fontSize = '11px';
      statusBadge.style.fontWeight = '700';
      statusBadge.style.textTransform = 'uppercase';
      statusBadge.style.padding = '4px 8px';
      statusBadge.style.borderRadius = '999px';
      if (item.status === 'approved') {
        statusBadge.style.background = 'rgba(87, 217, 163, 0.15)';
        statusBadge.style.color = 'var(--green)';
        statusBadge.textContent = 'Approved (+200 XP)';
      } else if (item.status === 'rejected') {
        statusBadge.style.background = 'rgba(255, 107, 107, 0.15)';
        statusBadge.style.color = '#ff6b6b';
        statusBadge.textContent = 'Rejected';
      } else {
        statusBadge.style.background = 'rgba(255, 214, 10, 0.15)';
        statusBadge.style.color = 'var(--yellow)';
        statusBadge.textContent = 'Pending';
      }
      tdStatus.append(statusBadge);

      // Actions
      const tdActions = document.createElement('td');
      tdActions.style.padding = '14px 18px';
      tdActions.style.textAlign = 'right';

      if (item.status === 'pending') {
        const btnApprove = document.createElement('button');
        btnApprove.className = 'btn-primary-sm';
        btnApprove.style.marginRight = '8px';
        btnApprove.textContent = '✓ Approve';
        btnApprove.addEventListener('click', () => reviewShare(item.id, 'approve'));

        const btnReject = document.createElement('button');
        btnReject.className = 'btn-secondary-sm';
        btnReject.style.color = '#ff6b6b';
        btnReject.textContent = '✗ Reject';
        btnReject.addEventListener('click', () => reviewShare(item.id, 'reject'));

        tdActions.append(btnApprove, btnReject);
      } else {
        const reviewedNote = document.createElement('span');
        reviewedNote.className = 'muted';
        reviewedNote.style.fontSize = '12px';
        reviewedNote.textContent = 'Reviewed';
        tdActions.append(reviewedNote);
      }

      tr.append(tdStudent, tdLesson, tdPlatform, tdLink, tdDate, tdStatus, tdActions);
      tbody.append(tr);
    });
  }

  async function reviewShare(shareId, action) {
    try {
      const res = await fetch('/api/v1/admin/social-shares/review', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': 'Bearer ' + currentToken
        },
        body: JSON.stringify({ share_id: shareId, action })
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Failed to review share');
      loadShares();
    } catch (err) {
      alert('Review failed: ' + err.message);
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
