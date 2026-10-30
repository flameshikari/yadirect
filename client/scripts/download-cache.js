import { FALLBACK_CACHE_TTL_MS, getDownloadExpiresAt } from './download-expiry.js';
const STORAGE_KEY = 'yadirect.downloads.v2';
const MAX_ENTRIES = 256;

export function createDownloadCache({ storage, now = Date.now } = {}) {
  let entries = new Map();
  let snapshot;
  function reload() {
    if (!storage) return;
    try {
      const raw = storage.getItem(STORAGE_KEY) ?? '[]';
      if (raw === snapshot) return;
      const saved = JSON.parse(raw);
      if (Array.isArray(saved)) entries = new Map(saved.slice(-MAX_ENTRIES));
      snapshot = raw;
    } catch {
      // Invalid or unavailable browser storage must not prevent downloads.
    }
  }
  reload();

  function valid(entry) {
    try {
      const url = new URL(entry.href);
      return url.protocol === 'https:' && !url.username && !url.password
        && !url.port && /^downloader\.disk\.yandex\.(ru|com|net)$/.test(url.hostname)
        && Number.isFinite(entry.expiresAt) && entry.expiresAt > now()
        && entry.expiresAt <= (getDownloadExpiresAt(entry.href) ?? now() + FALLBACK_CACHE_TTL_MS);
    } catch {
      return false;
    }
  }

  function persist() {
    for (const [key, entry] of entries) {
      if (!valid(entry)) entries.delete(key);
    }
    try {
      const raw = JSON.stringify([...entries]);
      storage?.setItem(STORAGE_KEY, raw);
      snapshot = raw;
    } catch {
      // Keep an in-memory cache when storage is blocked or full.
    }
  }

  return {
    get(key) {
      reload();
      const entry = entries.get(key);
      if (valid(entry)) return entry;
      if (entries.delete(key)) persist();
      return null;
    },
    set(key, href, expiresAt, receipt) {
      reload();
      const deadline = getDownloadExpiresAt(href) ?? now() + FALLBACK_CACHE_TTL_MS;
      const entry = { href, expiresAt: Math.min(expiresAt ?? deadline, deadline) };
      if (typeof receipt === 'string' && receipt.length > 0 && receipt.length <= 128) entry.receipt = receipt;
      if (!valid(entry)) return null;
      entries.delete(key);
      entries.set(key, entry);
      if (entries.size > MAX_ENTRIES) entries.delete(entries.keys().next().value);
      persist();
      return entry;
    },
  };
}
