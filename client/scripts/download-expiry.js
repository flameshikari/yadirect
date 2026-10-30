export const FALLBACK_CACHE_TTL_MS = 60 * 60 * 1000;

export function getDownloadExpiresAt(href) {
  try {
    const url = new URL(href);
    if (url.protocol !== 'https:' || url.username || url.password
        || !/^downloader\.disk\.yandex\.(ru|com|net)$/.test(url.hostname)) return null;
    const match = url.pathname.match(/^\/disk\/[^/]+\/([0-9a-f]{8})\/[^/]+/i);
    return match ? Number.parseInt(match[1], 16) * 1000 : null;
  } catch {
    return null;
  }
}
