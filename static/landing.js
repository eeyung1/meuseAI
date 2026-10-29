(() => {
  'use strict';

  const $ = id => document.getElementById(id);

  function previewTracks(){
    const count=Number(!!$('preview-landing')?.checked)+Number(!!$('preview-video')?.checked);
    if($('preview-track-total'))$('preview-track-total').textContent=count?`${count} track${count>1?'s':''}: ₦${(5000*count).toLocaleString()} · With earned 10% XP discount: ₦${(4500*count).toLocaleString()}`:'Select one or both tracks to see tuition.';
  }
  $('preview-landing')?.addEventListener('change',previewTracks);
  $('preview-video')?.addEventListener('change',previewTracks);
  previewTracks();

  function setFeedback(message, type = '') {
    const el = $('form-feedback');
    if (!el) return;
    if (!message) {
      el.hidden = true;
      el.className = 'feedback';
      el.textContent = '';
      return;
    }
    el.hidden = false;
    el.className = 'feedback ' + type;
    el.textContent = message;
  }

  function selectTab(tab) {
    const signupBtn = $('tab-signup');
    const loginBtn = $('tab-login');
    const signupPanel = $('panel-signup');
    const loginPanel = $('panel-login');
    setFeedback('');

    if (tab === 'login') {
      signupBtn?.classList.remove('active');
      signupBtn?.setAttribute('aria-selected', 'false');
      loginBtn?.classList.add('active');
      loginBtn?.setAttribute('aria-selected', 'true');

      if (signupPanel) signupPanel.hidden = true;
      if (loginPanel) loginPanel.hidden = false;
      const loginEmail = $('login-email');
      if (loginEmail) loginEmail.focus();
    } else {
      loginBtn?.classList.remove('active');
      loginBtn?.setAttribute('aria-selected', 'false');
      signupBtn?.classList.add('active');
      signupBtn?.setAttribute('aria-selected', 'true');

      if (loginPanel) loginPanel.hidden = true;
      if (signupPanel) signupPanel.hidden = false;
      const signupName = $('signup-name');
      if (signupName) signupName.focus();
    }
  }

  // Initialize tabs and check URL parameters
  function initTabs() {
    $('tab-signup')?.addEventListener('click', () => selectTab('signup'));
    $('tab-login')?.addEventListener('click', () => selectTab('login'));
    $('nav-login-btn')?.addEventListener('click', (e) => {
      e.preventDefault();
      selectTab('login');
      $('join')?.scrollIntoView({ behavior: 'smooth' });
    });

    const params = new URLSearchParams(window.location.search);
    if (params.get('tab') === 'login' || window.location.pathname === '/login') {
      selectTab('login');
    } else {
      selectTab('signup');
    }
  }

  // Handle Signup Form
  function initSignup() {
    const form = $('signup-form');
    if (!form) return;

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      setFeedback('');

      const name = $('signup-name')?.value.trim();
      const email = $('signup-email')?.value.trim().toLowerCase();
      const consent = $('signup-consent')?.checked;
      const submitBtn = $('signup-submit');

      if (!name) {
        setFeedback('Please enter your full name.', 'error');
        $('signup-name')?.focus();
        return;
      }
      if (!email || !email.includes('@')) {
        setFeedback('Please enter a valid email address.', 'error');
        $('signup-email')?.focus();
        return;
      }
      if (!consent) {
        setFeedback('Please check the consent box to receive curriculum emails.', 'error');
        return;
      }

      if (submitBtn) {
        submitBtn.disabled = true;
        submitBtn.textContent = 'Reserving your seat…';
      }

      try {
        const res = await fetch('/api/v1/students/signup', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ full_name: name, email: email, consent: true })
        });
        const data = await res.json().catch(() => ({}));

        if (res.ok || res.status === 202) {
          form.reset();
          setFeedback('✦ Verification email sent! Please check your inbox (and spam folder) within 24 hours to activate your account and start Day 1.', 'success');
        } else {
          setFeedback(data.error || 'Signup could not be processed. Please try again.', 'error');
        }
      } catch (err) {
        setFeedback('Network connection error. Please check your internet and try again.', 'error');
      } finally {
        if (submitBtn) {
          submitBtn.disabled = false;
          submitBtn.textContent = 'Start Day 1 Free →';
        }
      }
    });
  }

  // Handle Login / Magic Link Form
  function initLogin() {
    const form = $('login-form');
    if (!form) return;

    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      setFeedback('');

      const email = $('login-email')?.value.trim().toLowerCase();
      const submitBtn = $('login-submit');

      if (!email || !email.includes('@')) {
        setFeedback('Please enter a valid email address.', 'error');
        $('login-email')?.focus();
        return;
      }

      if (submitBtn) {
        submitBtn.disabled = true;
        submitBtn.textContent = 'Sending link…';
      }

      try {
        const res = await fetch('/api/v1/students/magic-link', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email: email })
        });
        const data = await res.json().catch(() => ({}));

        if (res.ok || res.status === 202) {
          form.reset();
          setFeedback('✦ Sign-in link sent! If an active account matches that email, check your inbox to access your learning hub.', 'success');
        } else {
          setFeedback(data.error || 'Could not send sign-in link. Please try again.', 'error');
        }
      } catch (err) {
        setFeedback('Network connection error. Please try again.', 'error');
      } finally {
        if (submitBtn) {
          submitBtn.disabled = false;
          submitBtn.textContent = 'Send Magic Sign-In Link →';
        }
      }
    });

    // Handle Resend Verification Click
    $('trigger-resend')?.addEventListener('click', async () => {
      const email = $('login-email')?.value.trim().toLowerCase();
      if (!email || !email.includes('@')) {
        setFeedback('Please enter your email above first, then click resend verification.', 'error');
        $('login-email')?.focus();
        return;
      }

      setFeedback('Requesting fresh verification link…');
      try {
        const res = await fetch('/api/v1/students/resend-verification', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email: email })
        });
        const data = await res.json().catch(() => ({}));
        if (res.ok || res.status === 202) {
          setFeedback('✦ If an unverified account exists, a fresh verification link has been sent to your inbox.', 'success');
        } else {
          setFeedback(data.error || 'Could not resend link. Please try again.', 'error');
        }
      } catch (err) {
        setFeedback('Network connection error. Please try again.', 'error');
      }
    });
  }

  document.addEventListener('DOMContentLoaded', () => {
    initTabs();
    initSignup();
    initLogin();
  });
})();
