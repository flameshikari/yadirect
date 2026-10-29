import { describe, expect, test } from 'bun:test';
import { createDownloadCache, CACHE_TTL_MS } from '../src/public/download-cache.js';
import { parseLink } from '../src/public/parse-link.js';

const href = 'https://downloader.disk.yandex.ru/download/example';
function memoryStorage() {
  const data = new Map();
  return { getItem: key => data.get(key) ?? null, setItem: (key, value) => data.set(key, value) };
}

describe('download cache', () => {
  test('restores links after reload, ignoring whitespace and equivalent public link formats', () => {
    const storage = memoryStorage();
    const cache = createDownloadCache({ storage, now: () => 1000 });
    cache.set(parseLink('  https://disk.yandex.ru/d/hash  '), href);
    const restored = createDownloadCache({ storage, now: () => 2000 });
    expect(restored.get(parseLink('\n disk.yandex.ru/d/hash \t'))).toEqual({ href, expiresAt: 1000 + CACHE_TTL_MS });
    expect(restored.get('/d/other')).toBeNull();
  });

  test('expires exactly one hour after processing without extending expiry on reuse', () => {
    let time = 1000;
    const cache = createDownloadCache({ now: () => time });
    cache.set('/d/hash', href);
    time += CACHE_TTL_MS - 1;
    expect(cache.get('/d/hash').expiresAt).toBe(1000 + CACHE_TTL_MS);
    time += 1;
    expect(cache.get('/d/hash')).toBeNull();
  });

  test('uses server expiry when it is sooner and caps longer expiry at one hour', () => {
    const cache = createDownloadCache({ now: () => 1000 });
    expect(cache.set('/d/hash', href, 2000).expiresAt).toBe(2000);
    expect(cache.set('/d/hash', href, 10_000_000).expiresAt).toBe(1000 + CACHE_TTL_MS);
    expect(cache.set('/d/expired', href, 999)).toBeNull();
  });

  test('works when browser storage is unavailable, full or corrupt', () => {
    const storage = { getItem() { throw new Error('blocked'); }, setItem() { throw new Error('full'); } };
    const cache = createDownloadCache({ storage });
    cache.set('/d/hash', href);
    expect(cache.get('/d/hash').href).toBe(href);
    const corrupt = createDownloadCache({ storage: { getItem: () => 'invalid JSON' } });
    expect(corrupt.get('/d/hash')).toBeNull();
  });

  test('rejects unsafe URLs and invalid cache entries', () => {
    const cache = createDownloadCache();
    for (const url of ['javascript:alert(1)', 'http://example.com', 'https://user:pass@example.com']) {
      expect(cache.set('/d/hash', url)).toBeNull();
    }
    const storage = memoryStorage();
    storage.setItem('yadirect.downloads.v1', JSON.stringify([['/d/hash', {href:'javascript:alert(1)', expiresAt:Date.now()+1000}]]));
    expect(createDownloadCache({ storage }).get('/d/hash')).toBeNull();
  });

  test('keeps at most 256 recent entries', () => {
    const cache = createDownloadCache();
    for (let i = 0; i < 257; i++) cache.set(`/d/${i}`, href);
    expect(cache.get('/d/0')).toBeNull();
    expect(cache.get('/d/256').href).toBe(href);
  });
});
