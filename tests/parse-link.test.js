import { describe, expect, test } from 'bun:test';
import { parseLink } from '../client/scripts/parse-link.js';

describe('link validation', () => {
  for (const key of ['-goNh3dqsHqxcA', '_goNh3dqsHqxcA', '0gRKgtGjqWil4g', '0123456789AB_-']) {
    test(`accepts the 14-character ID ${key}`, () => expect(parseLink(`disk.yandex.ru/i/${key}`)).toBe(`/i/${key}`));
  }
  for (const key of ['a'.repeat(13), 'a'.repeat(15), 'a'.repeat(13) + '!', 'a'.repeat(13) + '+', 'a'.repeat(13) + '=', 'a'.repeat(13) + 'я']) {
    test(`rejects invalid ID ${key}`, () => expect(() => parseLink(`disk.yandex.ru/d/${key}`)).toThrow());
  }

  for (const value of ['https://disk.yandex.ru/d/a-b_cD12345678', 'disk.yandex.ru/d/a-b_cD12345678', '/d/a-b_cD12345678']) {
    test(`accepts ${value}`, () => expect(parseLink(` ${value} `)).toBe('/d/a-b_cD12345678'));
  }
  test('removes tracking query strings while preserving encoded paths', () => {
    expect(parseLink('https://disk.yandex.com/i/-goNh3dqsHqxcA/folder%20name?foo=bar#fragment')).toBe('/i/-goNh3dqsHqxcA/folder%20name');
  });
  for (const value of ['', 'hello', 'https://disk.yandex.ru.evil.test/d/-goNh3dqsHqxcA', 'https://evil.test/d/-goNh3dqsHqxcA', '//evil.test/d/-goNh3dqsHqxcA', 'javascript:alert(1)', 'https://user@disk.yandex.ru/d/-goNh3dqsHqxcA', 'https://disk.yandex.ru/nope', '/d/-goNh3dqsHqxcA/%ZZ']) {
    test(`rejects ${JSON.stringify(value)}`, () => expect(() => parseLink(value)).toThrow());
  }
});
