(() => {
  'use strict';
  const params = new URLSearchParams(window.location.search);
  const isError = params.get('error') === 'expired' || params.get('error') === 'invalid';
  const successEl = document.getElementById('state-success');
  const errorEl = document.getElementById('state-error');

  if (isError) {
    if (successEl) successEl.hidden = true;
    if (errorEl) errorEl.hidden = false;

    const resendForm = document.getElementById('resend-form');
    const resendMsg = document.getElementById('resend-msg');
    const resendBtn = document.getElementById('btn-resend');

    resendForm?.addEventListener('submit', async (e) => {
      e.preventDefault();
      const email = document.getElementById('resend-email')?.value.trim().toLowerCase();
      if (!email) return;

      resendBtn.disabled = true;
      resendBtn.textContent = 'Sending…';

      try {
        const res = await fetch('/api/v1/students/resend-verification', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email })
        });
        resendMsg.hidden = false;
        resendMsg.className = 'feedback success';
        resendMsg.textContent = '✦ A fresh verification link has been sent to your email.';
        resendForm.reset();
      } catch (err) {
        resendMsg.hidden = false;
        resendMsg.className = 'feedback error';
        resendMsg.textContent = 'Could not send verification email. Please try again.';
      } finally {
        resendBtn.disabled = false;
        resendBtn.textContent = 'Send Fresh Verification Link →';
      }
    });
  } else {
    // Auto-redirect to dashboard on success
    setTimeout(() => {
      window.location.href = '/dashboard';
    }, 1800);
  }
})();
