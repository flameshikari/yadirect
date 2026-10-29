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
      || !/^\/[di]\/[\w-]+(?:\/.*)?$/.test(url.pathname)) {
    throw new Error('Use a public link such as https://disk.yandex.ru/d/your-file.');
  }
  try {
    decodeURIComponent(url.pathname);
  } catch {
    throw new Error('The link contains invalid URL encoding. Please copy it again.');
  }
  return url.pathname;
}
