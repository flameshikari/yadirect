import { describe, expect, test } from 'bun:test';
import { createDownloadCache } from '../client/scripts/download-cache.js';
import { FALLBACK_CACHE_TTL_MS, getDownloadExpiresAt } from '../client/scripts/download-expiry.js';
import { parseLink } from '../client/scripts/parse-link.js';

const expiresAt = (Math.floor(Date.now() / 1000) + 6 * 60 * 60) * 1000;
const href = `https://downloader.disk.yandex.ru/disk/signature/${(expiresAt / 1000).toString(16)}/token?filename=file.txt`;
function memoryStorage() {
  const data = new Map();
  return { getItem: key => data.get(key) ?? null, setItem: (key, value) => data.set(key, value) };
}

describe('download cache', () => {
  test('restores links after reload, ignoring whitespace and equivalent public link formats', () => {
    const storage = memoryStorage();
    const cache = createDownloadCache({ storage, now: () => 1000 });
    cache.set(parseLink('  https://disk.yandex.ru/d/-goNh3dqsHqxcA  '), href);
    const restored = createDownloadCache({ storage, now: () => 2000 });
    expect(restored.get(parseLink('\n disk.yandex.ru/d/-goNh3dqsHqxcA \t'))).toEqual({ href, expiresAt });
    expect(restored.get('/d/other')).toBeNull();
  });

  test('keeps links past one hour and expires exactly at the URL deadline', () => {
    let time = expiresAt - 6 * 60 * 60 * 1000;
    const cache = createDownloadCache({ now: () => time });
    cache.set('/d/-goNh3dqsHqxcA', href);
    time += 3_600_000;
    expect(cache.get('/d/-goNh3dqsHqxcA').expiresAt).toBe(expiresAt);
    time = expiresAt - 1;
    expect(cache.get('/d/-goNh3dqsHqxcA').href).toBe(href);
    time += 1;
    expect(cache.get('/d/-goNh3dqsHqxcA')).toBeNull();
  });

  test('never caches beyond the URL deadline, even with a later server deadline', () => {
    const cache = createDownloadCache({ now: () => 1000 });
    expect(cache.set('/d/-goNh3dqsHqxcA', href, 2000).expiresAt).toBe(2000);
    expect(cache.set('/d/-goNh3dqsHqxcA', href, expiresAt + 10_000).expiresAt).toBe(expiresAt);
    expect(cache.set('/d/expired', href, 999)).toBeNull();
    expect(createDownloadCache({ now: () => expiresAt }).set('/d/expired', href)).toBeNull();
  });

  test('uses fallback TTL only for unrecognized URLs', () => {
    const cache = createDownloadCache({ now: () => 1000 });
    expect(cache.set('/d/-goNh3dqsHqxcA', 'https://downloader.disk.yandex.ru/download/example').expiresAt).toBe(1000 + FALLBACK_CACHE_TTL_MS);
  });

  test('reads the two provided HEX timestamps as absolute Unix deadlines', () => {
    expect(getDownloadExpiresAt('https://downloader.disk.yandex.ru/disk/signature/6ae45191/token')).toBe(1_793_347_985_000);
    expect(getDownloadExpiresAt('https://downloader.disk.yandex.ru/disk/signature/6ae452e2/token')).toBe(1_793_348_322_000);
    expect(getDownloadExpiresAt('https://downloader.disk.yandex.ru/disk/signature/6AE452E2/token?uid=0')).toBe(1_793_348_322_000);
    expect(getDownloadExpiresAt('https://example.com/disk/signature/6ae45191/token')).toBeNull();
    expect(getDownloadExpiresAt('https://downloader.disk.yandex.ru/disk/signature/invalid/token')).toBeNull();
  });

  test('rejects stored entries whose deadline exceeds the actual URL expiry', () => {
    const storage = memoryStorage();
    storage.setItem('yadirect.downloads.v2', JSON.stringify([['/d/-goNh3dqsHqxcA', { href, expiresAt: expiresAt + 1000 }]]));
    expect(createDownloadCache({ storage }).get('/d/-goNh3dqsHqxcA')).toBeNull();
  });

  test('works when browser storage is unavailable, full or corrupt', () => {
    const storage = { getItem() { throw new Error('blocked'); }, setItem() { throw new Error('full'); } };
    const cache = createDownloadCache({ storage });
    cache.set('/d/-goNh3dqsHqxcA', href);
    expect(cache.get('/d/-goNh3dqsHqxcA').href).toBe(href);
    const corrupt = createDownloadCache({ storage: { getItem: () => 'invalid JSON' } });
    expect(corrupt.get('/d/-goNh3dqsHqxcA')).toBeNull();
  });

  test('rejects unsafe URLs and invalid cache entries', () => {
    const cache = createDownloadCache();
    for (const url of ['javascript:alert(1)', 'http://example.com', 'https://user:pass@example.com']) {
      expect(cache.set('/d/-goNh3dqsHqxcA', url)).toBeNull();
    }
    const storage = memoryStorage();
    storage.setItem('yadirect.downloads.v2', JSON.stringify([['/d/-goNh3dqsHqxcA', {href:'javascript:alert(1)', expiresAt:Date.now()+1000}]]));
    expect(createDownloadCache({ storage }).get('/d/-goNh3dqsHqxcA')).toBeNull();
  });

  test('keeps at most 256 recent entries', () => {
    const cache = createDownloadCache();
    for (let i = 0; i < 257; i++) cache.set(`/d/${i}`, href);
    expect(cache.get('/d/0')).toBeNull();
    expect(cache.get('/d/256').href).toBe(href);
  });
});

test('cache shares rotated server receipts between tabs and across reloads', () => {
  const storage = memoryStorage();
  const first = createDownloadCache({ storage });
  const second = createDownloadCache({ storage });
  const path = '/d/-goNh3dqsHqxcA';
  first.set(path, href, expiresAt, 'first-receipt');
  expect(second.get(path).receipt).toBe('first-receipt');
  second.set(path, href, expiresAt, 'second-receipt');
  expect(first.get(path).receipt).toBe('second-receipt');
  expect(createDownloadCache({ storage }).get(path).receipt).toBe('second-receipt');
  // Old entries remain downloadable and obtain a server receipt only on a download click.
  first.set(path, href, expiresAt);
  expect(second.get(path).href).toBe(href);
  expect(second.get(path).receipt).toBeUndefined();
});
