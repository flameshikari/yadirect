const enabled = document.querySelector('meta[name="yadirect-counter"]')?.content === 'enabled';
const counter = document.querySelector('.counter');
const count = document.querySelector('#converted-count');
counter.hidden = !enabled;
const formatter = new Intl.NumberFormat('en-US');
const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
let currentCount = -1;
let revision = 0;
let requestSequence = 0;
let animations = [];
let socket;
let reconnectTimer;
let reconnectDelay = 500;
let leaving = false;
let messageID = 0;
const pending = new Map();

export function renderStats(stats, { allowDecrease = false } = {}) {
  const value = stats?.convertedLinks;
  if (!Number.isSafeInteger(value) || value < 0 || value === currentCount) return;
  if (!allowDecrease && value < currentCount) return;
  const previous = currentCount;
  currentCount = value;
  const currentRevision = ++revision;
  for (const animation of animations) animation.cancel();
  animations = [];
  count.dataset.value = String(value);
  counter.dataset.ready = '';
  const incoming = document.createElement('span');
  incoming.textContent = formatter.format(value);
  if (previous < 0 || reducedMotion.matches) {
    count.replaceChildren(incoming);
    return;
  }
  const outgoing = document.createElement('span');
  outgoing.textContent = formatter.format(previous);
  outgoing.setAttribute('aria-hidden', 'true');
  count.replaceChildren(outgoing, incoming);
  animations = [
    outgoing.animate([
      { opacity: 1, transform: 'translateY(0) scaleY(1)' },
      { opacity: .45, transform: 'translateY(35%) scaleY(1.3)', offset: .5 },
      { opacity: 0, transform: 'translateY(100%) scaleY(1.55)' },
    ], { duration: 280, easing: 'cubic-bezier(.4,0,.6,1)', fill: 'both' }),
    incoming.animate([
      { opacity: 0, transform: 'translateY(-120%) scaleY(1.8)' },
      { opacity: 1, transform: 'translateY(0) scaleY(1)' },
    ], { duration: 220, delay: 35, easing: 'cubic-bezier(.16,1,.3,1)', fill: 'both' }),
  ];
  Promise.all(animations.map(animation => animation.finished)).then(() => {
    if (revision === currentRevision) {
      count.replaceChildren(incoming);
      animations = [];
    }
  }, () => { /* A newer value replaced this animation. */ });
}

function connect() {
  if (!enabled || leaving || typeof WebSocket !== 'function') return;
  if (socket && socket.readyState < WebSocket.CLOSING) return;
  const connection = socket = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/api/stats/ws`);
  connection.addEventListener('open', () => { reconnectDelay = 500; });
  connection.addEventListener('message', (event) => {
    let message;
    try { message = JSON.parse(event.data); } catch { return; }
    const request = pending.get(message.id);
    if (message.type === 'stats') renderStats(message, { allowDecrease: request?.type === 'get' });
    if (request) {
      pending.delete(message.id);
      clearTimeout(request.timer);
      if (message.type === 'error') request.reject(Object.assign(new Error(message.message), { sent: true, code: message.error }));
      else request.resolve(message);
    }
  });
  connection.addEventListener('close', () => {
    for (const [id, request] of pending) {
      if (request.connection !== connection) continue;
      clearTimeout(request.timer);
      request.reject(Object.assign(new Error('WebSocket closed'), { sent: true }));
      pending.delete(id);
    }
    if (socket !== connection) return;
    if (!leaving) {
      if (currentCount < 0) void refreshHTTP();
      clearTimeout(reconnectTimer);
      reconnectTimer = setTimeout(connect, reconnectDelay);
      reconnectDelay = Math.min(reconnectDelay * 2, 30_000);
    }
  });
}

async function sendCommand(type, payload = {}) {
  connect();
  const connection = socket;
  if (!connection || connection.readyState > WebSocket.OPEN) throw new Error('WebSocket unavailable');
  if (connection.readyState === WebSocket.CONNECTING) {
    await new Promise((resolve, reject) => {
      const cleanup = () => {
        clearTimeout(timer);
        connection.removeEventListener('open', opened);
        connection.removeEventListener('close', closed);
        connection.removeEventListener('error', closed);
      };
      const opened = () => { cleanup(); resolve(); };
      const closed = () => { cleanup(); reject(new Error('WebSocket unavailable')); };
      const timer = setTimeout(closed, 1000);
      connection.addEventListener('open', opened, { once: true });
      connection.addEventListener('close', closed, { once: true });
      connection.addEventListener('error', closed, { once: true });
    });
  }
  return new Promise((resolve, reject) => {
    const id = ++messageID;
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(Object.assign(new Error('Statistics request timed out'), { sent: true }));
    }, 3000);
    pending.set(id, { type, resolve, reject, timer, connection });
    try { connection.send(JSON.stringify({ type, id, ...payload })); }
    catch (error) { pending.delete(id); clearTimeout(timer); reject(error); }
  });
}

async function refreshHTTP() {
  if (!enabled) return;
  const startedAtRevision = revision;
  const sequence = ++requestSequence;
  try {
    const response = await fetch('/api/stats', { cache: 'no-store', signal: AbortSignal.timeout(3000) });
    if (!response.ok) return;
    const stats = await response.json();
    if (sequence === requestSequence && startedAtRevision === revision) renderStats(stats, { allowDecrease: true });
  } catch { /* Retain the last value until the next user action or reconnection. */ }
}

export async function refreshCounter() {
  if (!enabled) return;
  try { await sendCommand('get'); }
  catch { await refreshHTTP(); }
}

export async function recordCacheHit(publicPath, entry) {
  if (!enabled) return;
  // Old cached entries remain downloadable; validate them with the server on the first click.
  if (!entry.receipt) {
    try {
      const response = await fetch(`/api/download${publicPath}`, { cache: 'no-store', signal: AbortSignal.timeout(15_000) });
      if (!response.ok) return;
      const next = await response.json();
      void refreshCounter();
      return next;
    } catch { return; }
  }
  const payload = { publicPath, href: entry.href, expiresAt: entry.expiresAt, receipt: entry.receipt };
  const updated = message => {
    renderStats(message);
    return { ...entry, receipt: message.receipt };
  };
  const failed = error => {
    if (['INVALID_RECEIPT', 'EXPIRED_RECEIPT', 'RECEIPT_REPLAYED'].includes(error.code)) return { ...entry, receipt: '' };
  };
  try { return updated(await sendCommand('cache-hit', payload)); }
  catch (error) {
    // Never repeat a write that may already have reached the server.
    if (error.sent) { void refreshCounter(); return failed(error); }
    try {
      const response = await fetch('/api/stats/cache-hit', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload), keepalive: true, signal: AbortSignal.timeout(3000),
      });
      const message = await response.json();
      if (response.ok) return updated(message);
      return failed({ code: message.error });
    } catch { /* Statistics must not block a valid download. */ }
  }
}

connect();
if (typeof WebSocket !== 'function') void refreshHTTP();
window.addEventListener('pageshow', () => { leaving = false; connect(); });
window.addEventListener('pagehide', () => { leaving = true; clearTimeout(reconnectTimer); socket?.close(); });
window.addEventListener('online', () => { clearTimeout(reconnectTimer); connect(); });
document.addEventListener('visibilitychange', () => { if (!document.hidden) void refreshCounter(); });
