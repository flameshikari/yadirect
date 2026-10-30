export const PUBLIC_KEY_LENGTH = 14;
export const PUBLIC_KEY_PATTERN = /^[A-Za-z0-9_-]{14}$/;

export function parseLink(value) {
  const input = value.trim();
  if (!input) throw new Error('Paste a public Yandex.Disk link first.');
  let url;
  try {
    url = input.startsWith('/') && !input.startsWith('//')
      ? new URL(input, 'https://disk.yandex.ru')
      : new URL(/^https?:\/\//i.test(input) ? input : `https://${input}`);
  } catch {
    throw new Error('Enter a valid public Yandex.Disk link.');
  }
  if (!['https:', 'http:'].includes(url.protocol)
      || !['disk.yandex.ru', 'disk.yandex.com', 'yadi.sk'].includes(url.hostname)
      || url.username || url.password || url.port
      || !/^\/[di]\/[^/]+(?:\/.*)?$/.test(url.pathname)
      || !PUBLIC_KEY_PATTERN.test(url.pathname.split('/')[2])) {
    throw new Error('Use a public Yandex.Disk link with a 14-character file ID.');
  }
  try {
    decodeURIComponent(url.pathname);
  } catch {
    throw new Error('The link contains invalid URL encoding. Please copy it again.');
  }
  return url.pathname;
}
