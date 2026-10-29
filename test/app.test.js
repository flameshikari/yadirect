import { afterEach, describe, expect, mock, test } from 'bun:test';
import { createApp } from '../src/app.js';
import { parseLink } from '../src/public/parse-link.js';

const servers = [];
afterEach(async () => {
  await Promise.all(servers.splice(0).map((server) => new Promise((resolve) => {
    server.closeAllConnections();
    server.close(resolve);
  })));
});

async function start(options = {}) {
  const server = createApp(options).listen(0, '127.0.0.1');
  servers.push(server);
  await new Promise((resolve) => server.once('listening', resolve));
  return (path, options) => fetch(`http://127.0.0.1:${server.address().port}${path}`, options);
}
const json = (value, status = 200) => Response.json(value, { status });
const href = 'https://downloader.disk.yandex.ru/download/example';

describe('HTTP API', () => {
  test('serves the frontend, assets and health from any working directory', async () => {
    const request = await start();
    const previous = process.cwd();
    try {
      process.chdir('/tmp');
      const page = await request('/');
      expect(page.status).toBe(200);
      expect(await page.text()).toContain('download-form');
      expect(page.headers.get('content-security-policy')).toContain("script-src 'self'");
      expect((await request('/script.js')).status).toBe(200);
      expect(await (await request('/health')).json()).toEqual({ status: 'ok' });
    } finally {
      process.chdir(previous);
    }
  });

  test('redirects direct links and shares cached results with the form API', async () => {
    const fetchImpl = mock(async () => json({ href }));
    const request = await start({ fetchImpl, now: () => 1000 });
    const response = await request('/d/hash-with_dash', { redirect: 'manual' });
    expect(response.status).toBe(302);
    expect(response.headers.get('location')).toBe(href);
    const api = await request('/api/download/d/hash-with_dash');
    expect(await api.json()).toEqual({ href, expiresAt: 3_601_000 });
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    expect(new URL(fetchImpl.mock.calls[0][0]).searchParams.get('public_key')).toBe('https://disk.yandex.ru/d/hash-with_dash');
  });

  test('keeps download links for one hour without extending expiry on cache hits', async () => {
    let currentTime = 1000;
    const fetchImpl = mock(async () => json({ href }));
    const request = await start({ fetchImpl, now: () => currentTime });
    const first = await (await request('/api/download/d/hash')).json();
    currentTime += 3_599_999;
    expect(await (await request('/api/download/d/hash')).json()).toEqual(first);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
    currentTime += 1;
    const refreshed = await (await request('/api/download/d/hash')).json();
    expect(refreshed.expiresAt).toBe(currentTime + 3_600_000);
    expect(fetchImpl).toHaveBeenCalledTimes(2);
  });

  test('preserves Unicode, spaces and ampersands in folder paths without query injection', async () => {
    const items = [{ name: 'file.txt', type: 'file' }];
    const fetchImpl = mock(async () => json({ _embedded: { items } }));
    const request = await start({ fetchImpl });
    const response = await request(`/list/i/hash/${encodeURIComponent('Папка & фото')}/file.txt?ignored=yes`);
    expect(await response.json()).toEqual(items);
    const url = new URL(fetchImpl.mock.calls[0][0]);
    expect(url.searchParams.get('path')).toBe('/Папка & фото/file.txt');
    expect([...url.searchParams.keys()]).toEqual(['public_key', 'path']);
  });

  for (const status of [400, 403, 404, 429, 500]) {
    test(`handles upstream HTTP ${status} even with a non-JSON response, without caching errors`, async () => {
      const fetchImpl = mock(async () => new Response('upstream failure', { status }));
      const request = await start({ fetchImpl });
      const response = await request('/d/hash', { redirect: 'manual' });
      expect(response.status).toBe(status === 500 ? 502 : status);
      expect((await response.json()).message).toBeString();
      await request('/d/hash');
      expect(fetchImpl).toHaveBeenCalledTimes(2);
    });
  }

  test('returns 502 for network failures without leaking internal details', async () => {
    const request = await start({ fetchImpl: async () => { throw new TypeError('secret internal address'); } });
    const response = await request('/api/download/d/hash');
    expect(response.status).toBe(502);
    expect(await response.text()).not.toContain('secret');
  });

  test('aborts slow upstream requests with HTTP 504', async () => {
    const fetchImpl = async (url, { signal }) => new Promise((resolve, reject) => {
      signal.addEventListener('abort', () => reject(signal.reason), { once: true });
    });
    const request = await start({ fetchImpl, timeoutMs: 20 });
    const response = await request('/api/download/d/hash');
    expect(response.status).toBe(504);
    expect((await response.json()).error).toBe('UPSTREAM_TIMEOUT');
  });

  for (const body of [{}, null, { href: 'javascript:alert(1)' }, { href: 'http://example.com' }, { href: 'https://user:pass@example.com' }]) {
    test(`rejects malformed download response ${JSON.stringify(body)}`, async () => {
      const request = await start({ fetchImpl: async () => json(body) });
      expect((await request('/d/hash', { redirect: 'manual' })).status).toBe(502);
    });
  }

  test('rejects invalid JSON and malformed folder listings', async () => {
    const invalidJson = await start({ fetchImpl: async () => new Response('not json') });
    expect((await invalidJson('/d/hash')).status).toBe(502);
    const invalidList = await start({ fetchImpl: async () => json({}) });
    expect((await invalidList('/list/d/hash')).status).toBe(502);
  });

  test('returns proper errors for malformed encoding and unknown routes', async () => {
    const fetchImpl = mock();
    const request = await start({ fetchImpl });
    expect((await request('/d/hash/%ZZ')).status).toBe(400);
    expect((await request('/not-a-link')).status).toBe(404);
    expect(fetchImpl).not.toHaveBeenCalled();
  });
});

describe('link validation', () => {
  for (const value of ['https://disk.yandex.ru/d/a-b_c', 'disk.yandex.ru/d/a-b_c', '/d/a-b_c']) {
    test(`accepts ${value}`, () => expect(parseLink(` ${value} `)).toBe('/d/a-b_c'));
  }
  test('removes tracking query strings while preserving encoded paths', () => {
    expect(parseLink('https://disk.yandex.com/i/hash/folder%20name?foo=bar#fragment')).toBe('/i/hash/folder%20name');
  });
  for (const value of ['', 'hello', 'https://disk.yandex.ru.evil.test/d/hash', 'https://evil.test/d/hash', '//evil.test/d/hash', 'javascript:alert(1)', 'https://user@disk.yandex.ru/d/hash', 'https://disk.yandex.ru/nope', '/d/hash/%ZZ']) {
    test(`rejects ${JSON.stringify(value)}`, () => expect(() => parseLink(value)).toThrow());
  }
});
