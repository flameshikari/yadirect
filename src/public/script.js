import { parseLink } from './parse-link.js';
import { createDownloadCache } from './download-cache.js';

const form = document.querySelector('#download-form');
const input = document.querySelector('#text');
const button = form.querySelector('button');
const downloadButton = document.querySelector('#download-button');
const actionSlot = document.querySelector('.action-slot');
const placeholderAlphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-';
const placeholderKeyLength = '0gRKgtGjqWil4g'.length;
const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
const placeholder = document.querySelector('.placeholder');
placeholder.textContent = input.placeholder;
placeholder.hidden = false;
input.parentElement.classList.add('placeholder-enhanced');
let placeholderType = 'i';

setInterval(() => {
  if (input.value || document.activeElement === input || document.hidden || reducedMotion.matches) return;
  placeholderType = placeholderType === 'i' ? 'd' : 'i';
  const key = Array.from({ length: placeholderKeyLength }, () =>
    placeholderAlphabet[Math.floor(Math.random() * placeholderAlphabet.length)],
  ).join('');
  input.placeholder = `disk.yandex.ru/${placeholderType}/${key}`;
  placeholder.textContent = input.placeholder;
}, 250);

let storage;
try { storage = window.localStorage; } catch { /* Use the in-memory cache. */ }
const cache = createDownloadCache({ storage });
let pending = false;
let expiryTimer;

function showDownload(entry) {
  clearTimeout(expiryTimer);
  downloadButton.href = entry.href;
  actionSlot.dataset.mode = 'download';
  downloadButton.inert = false;
  downloadButton.removeAttribute('aria-hidden');
  button.inert = true;
  button.setAttribute('aria-hidden', 'true');
  button.disabled = true;
  form.dataset.state = 'success';
  expiryTimer = setTimeout(reset, Math.max(0, entry.expiresAt - Date.now()));
}

function reset() {
  if (pending) return;
  clearTimeout(expiryTimer);
  actionSlot.dataset.mode = 'process';
  downloadButton.inert = true;
  downloadButton.setAttribute('aria-hidden', 'true');
  downloadButton.removeAttribute('href');
  button.inert = false;
  button.removeAttribute('aria-hidden');
  form.dataset.state = 'idle';
  button.setAttribute('aria-label', 'Process link');
  try {
    const path = parseLink(input.value);
    button.disabled = false;
    input.setAttribute('aria-invalid', 'false');
    const entry = cache.get(path);
    if (entry) showDownload(entry);
  } catch {
    button.disabled = true;
    const invalid = input.value.trim() !== '';
    input.setAttribute('aria-invalid', String(invalid));
    button.setAttribute('aria-label', invalid ? 'Invalid link. Change the link.' : 'Process link');
  }
}

input.addEventListener('paste', (event) => {
  if (input.readOnly) return;
  const text = event.clipboardData?.getData('text/plain');
  if (!text) return;
  event.preventDefault();
  input.setRangeText(
    text.trim().replace(/^https?:\/\//i, ''),
    input.selectionStart ?? 0,
    input.selectionEnd ?? input.value.length,
    'end',
  );
  input.dispatchEvent(new Event('input', { bubbles: true }));
});
input.addEventListener('input', reset);
window.addEventListener('pageshow', reset);
reset();

form.addEventListener('submit', async (event) => {
  event.preventDefault();
  if (pending) return;
  if (actionSlot.dataset.mode === 'download') {
    downloadButton.click();
    return;
  }
  if (button.disabled) return;
  let invalidLink = false;
  try {
    const path = parseLink(input.value);
    pending = true;
    button.disabled = true;
    input.readOnly = true;
    form.setAttribute('aria-busy', 'true');
    form.dataset.state = 'loading';
    button.setAttribute('aria-label', 'Processing link');
    const response = await fetch(`/api/download${path}`, { signal: AbortSignal.timeout(15_000) });
    invalidLink = [400, 403, 404].includes(response.status);
    const data = await response.json();
    if (!response.ok) throw new Error(data?.message || 'Could not process link');
    const entry = cache.set(path, data.href, data.expiresAt);
    if (!entry) throw new Error('Invalid or expired download link');
    showDownload(entry);
    downloadButton.click();
  } catch {
    form.dataset.state = 'error';
    input.setAttribute('aria-invalid', String(invalidLink));
    button.setAttribute('aria-label', invalidLink ? 'Link unavailable. Change the link.' : 'Processing failed. Try again.');
  } finally {
    pending = false;
    button.disabled = actionSlot.dataset.mode === 'download' || invalidLink;
    input.readOnly = false;
    form.setAttribute('aria-busy', 'false');
  }
});
