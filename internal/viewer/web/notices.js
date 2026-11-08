const statusBox = document.getElementById('status');
const toast = document.getElementById('toast');
const toastDurationMs = 4000;
let toastTimer;

/** Shows a persistent message at the top of the page; empty hides it. */
export function setStatus(message, { error = false } = {}) {
  statusBox.textContent = message;
  statusBox.classList.toggle('is-error', error);
  statusBox.classList.toggle('is-hidden', !message);
}

/** Shows a short-lived confirmation at the bottom of the page. */
export function showToast(message) {
  toast.textContent = message;
  toast.classList.remove('is-hidden');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toast.classList.add('is-hidden'), toastDurationMs);
}
