import { describe, expect, test } from 'bun:test';
import { matchesInputMask, normalizePastedLink } from '../client/scripts/input-mask.js';

describe('link input mask', () => {
  for (const link of ['disk.yandex.ru/d/-goNh3dqsHqxcA', 'disk.yandex.com/i/0gRKgtGjqWil4g']) {
    test(`accepts every prefix of ${link}`, () => {
      for (let length = 0; length <= link.length; length++) expect(matchesInputMask(link.slice(0, length))).toBe(true);
    });
  }
  for (const value of ['h', 'disk.yandex.org', 'disk.yandex.rx', 'disk.yandex.ru/x', 'disk.yandex.ru/d/+','disk.yandex.ru/i/=', 'disk.yandex.ru/i/я', 'disk.yandex.ru/i/a b', 'disk.yandex.ru/d/0123456789abcde', 'disk.yandex.ru/d/-goNh3dqsHqxcA/path']) {
    test(`rejects ${JSON.stringify(value)}`, () => expect(matchesInputMask(value)).toBe(false));
  }
  test('normalizes pasted URLs without changing case-sensitive IDs', () => {
    expect(normalizePastedLink('  HTTPS://DISK.YANDEX.COM/d/-goNh3dqsHqxcA?utm_source=test#fragment  ')).toBe('disk.yandex.com/d/-goNh3dqsHqxcA');
    expect(normalizePastedLink('http://disk.yandex.ru/i/0gRKgtGjqWil4g')).toBe('disk.yandex.ru/i/0gRKgtGjqWil4g');
    expect(normalizePastedLink('ABC_12-')).toBe('ABC_12-');
    expect(matchesInputMask(normalizePastedLink('https://evil.test/d/-goNh3dqsHqxcA'))).toBe(false);
  });
});
