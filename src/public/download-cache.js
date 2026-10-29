export const CACHE_TTL_MS = 60 * 60 * 1000;
const STORAGE_KEY = 'yadirect.downloads.v1';
const MAX_ENTRIES = 256;

export function createDownloadCache({ storage, now = Date.now } = {}) {
  let entries = new Map();
  try {
    const saved = JSON.parse(storage?.getItem(STORAGE_KEY) ?? '[]');
    if (Array.isArray(saved)) entries = new Map(saved.slice(-MAX_ENTRIES));
  } catch {
    // Invalid or unavailable browser storage must not prevent downloads.
  }

  function valid(entry) {
    try {
      const url = new URL(entry.href);
      return url.protocol === 'https:' && !url.username && !url.password
        && Number.isFinite(entry.expiresAt) && entry.expiresAt > now()
        && entry.expiresAt <= now() + CACHE_TTL_MS;
    } catch {
      return false;
    }
  }

  function persist() {
    for (const [key, entry] of entries) {
      if (!valid(entry)) entries.delete(key);
    }
    try {
      storage?.setItem(STORAGE_KEY, JSON.stringify([...entries]));
    } catch {
      // Keep an in-memory cache when storage is blocked or full.
    }
  }

  return {
    get(key) {
      const entry = entries.get(key);
      if (valid(entry)) return entry;
      if (entries.delete(key)) persist();
      return null;
    },
    set(key, href, expiresAt = now() + CACHE_TTL_MS) {
      const entry = { href, expiresAt: Math.min(expiresAt, now() + CACHE_TTL_MS) };
      if (!valid(entry)) return null;
      entries.delete(key);
      entries.set(key, entry);
      if (entries.size > MAX_ENTRIES) entries.delete(entries.keys().next().value);
      persist();
      return entry;
    },
  };
}
