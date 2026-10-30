import { parseLink, PUBLIC_KEY_LENGTH } from './parse-link.js';
import { createDownloadCache } from './download-cache.js';
import { bindInputMask } from './input-mask.js';
import { refreshCounter, renderStats, recordCacheHit } from './stats.js';

const form = document.querySelector('#download-form');
const input = document.querySelector('#text');
const button = form.querySelector('button');
const downloadButton = document.querySelector('#download-button');
const actionSlot = document.querySelector('.action-slot');
const placeholderAlphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-';
const placeholderKeyLength = PUBLIC_KEY_LENGTH;
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
let skipNextDownloadHit = false;
let reportingHit = false;

function showDownload(entry, alreadyCounted = false) {
  skipNextDownloadHit = alreadyCounted;
  clearTimeout(expiryTimer);
  downloadButton.href = entry.href;
  actionSlot.dataset.mode = 'download';
  downloadButton.inert = false;
  downloadButton.removeAttribute('aria-hidden');
  button.inert = true;
  button.setAttribute('aria-hidden', 'true');
  button.disabled = true;
  form.dataset.state = 'success';
  expiryTimer = setTimeout(reset, Math.min(2_147_483_647, Math.max(0, entry.expiresAt - Date.now())));
}

function reset() {
  if (pending) return;
  clearTimeout(expiryTimer);
  skipNextDownloadHit = false;
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

downloadButton.addEventListener('click', (event) => {
  let entry;
  let path;
  try { path = parseLink(input.value); entry = cache.get(path); } catch { /* The input is no longer a public link. */ }
  if (!entry) {
    event.preventDefault();
    reset();
  } else if (skipNextDownloadHit) {
    skipNextDownloadHit = false;
  } else {
    if (!reportingHit) {
      reportingHit = true;
      void recordCacheHit(path, entry).then(next => {
        const current = cache.get(path);
        if (next && current?.href === entry.href && current?.receipt === entry.receipt) {
          cache.set(path, next.href, next.expiresAt, next.receipt);
        }
      }).finally(() => { reportingHit = false; });
    }
  }
});

bindInputMask(input);
input.addEventListener('input', reset);
input.addEventListener('keydown', (event) => {
  if (event.key !== 'Enter' || event.isComposing) return;
  event.preventDefault();
  if (!event.repeat && !pending) form.requestSubmit();
});
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
    const entry = cache.set(path, data.href, data.expiresAt, data.receipt);
    if (!entry) throw new Error('Invalid or expired download link');
    showDownload(entry, true);
    const convertedLinks = response.headers.get('X-Converted-Links');
    if (/^\d+$/.test(convertedLinks ?? '')) renderStats({ convertedLinks: Number(convertedLinks) });
    void refreshCounter();
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
