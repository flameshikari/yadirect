import { fileURLToPath } from 'node:url';
import cors from 'cors';
import express from 'express';
import helmet from 'helmet';

class HttpError extends Error {
  constructor(status, code, message) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

const upstreamMessages = {
  400: 'Yandex.Disk could not process this link or folder path.',
  403: 'Access to this file is restricted or its download limit has been reached.',
  404: 'This file was not found. Check that the link is public and still available.',
  429: 'Yandex.Disk is receiving too many requests. Please try again later.',
};

export function createApp({ fetchImpl = globalThis.fetch, timeoutMs = 10_000, now = Date.now } = {}) {
  const app = express();
  const cache = new Map();
  app.disable('x-powered-by');
  app.use(cors(), helmet({
    contentSecurityPolicy: {
      directives: { 'upgrade-insecure-requests': null },
    },
  }));
  app.get('/health', (req, res) => res.json({ status: 'ok' }));
  app.use(express.static(fileURLToPath(new URL('./public/', import.meta.url))));

  async function resolve(req, res) {
    const [, operation, type, hash, encodedPath] = req.path.match(
      /^\/(?:(list|api\/download)\/)?([di])\/([\w-]+)(?:\/(.*))?$/,
    );
    let folderPath;
    try {
      folderPath = encodedPath ? `/${decodeURIComponent(encodedPath)}` : null;
    } catch {
      throw new HttpError(400, 'INVALID_PATH', 'The folder path contains invalid URL encoding.');
    }
    const isList = operation === 'list';
    const url = new URL(`https://cloud-api.yandex.net/v1/disk/public/resources${isList ? '' : '/download'}`);
    url.searchParams.set('public_key', `https://disk.yandex.ru/${type}/${hash}`);
    if (folderPath) url.searchParams.set('path', folderPath);
    const key = url.href;
    let data = cache.get(key);
    if (!data || data.expires <= now()) {
      cache.delete(key);
      let response;
      let body;
      try {
        response = await fetchImpl(url, { signal: AbortSignal.timeout(timeoutMs) });
        if (response.ok) body = await response.json();
        else await response.body?.cancel();
      } catch (error) {
        if (error.name === 'TimeoutError' || error.name === 'AbortError') {
          throw new HttpError(504, 'UPSTREAM_TIMEOUT', 'Yandex.Disk took too long to respond. Please try again.');
        }
        throw new HttpError(502, 'UPSTREAM_UNAVAILABLE', 'Could not read a response from Yandex.Disk. Please try again.');
      }
      if (!response.ok) {
        const status = upstreamMessages[response.status] ? response.status : 502;
        throw new HttpError(status, 'UPSTREAM_ERROR', upstreamMessages[status] ?? 'Yandex.Disk is temporarily unavailable. Please try again.');
      }
      let value;
      if (isList) {
        value = body?._embedded?.items;
        if (!Array.isArray(value)) {
          throw new HttpError(502, 'INVALID_RESPONSE', 'Yandex.Disk did not return a folder listing.');
        }
      } else {
        try {
          const download = new URL(body.href);
          if (download.protocol !== 'https:' || download.username || download.password) throw new Error();
          value = download.href;
        } catch {
          throw new HttpError(502, 'INVALID_RESPONSE', 'Yandex.Disk did not return a valid download link.');
        }
      }
      data = { value, expires: now() + (isList ? 60_000 : 3_600_000) };
      if (cache.size >= 256) cache.delete(cache.keys().next().value);
      cache.set(key, data);
    }
    res.set('Cache-Control', 'no-store');
    if (isList) return res.json(data.value);
    if (operation === 'api/download') return res.json({ href: data.value, expiresAt: data.expires });
    return res.redirect(data.value);
  }

  app.get(/^\/(?:(list|api\/download)\/)?([di])\/([\w-]+)(?:\/(.*))?$/, resolve);
  app.use((req, res) => {
    res.status(404).json({ error: 'NOT_FOUND', message: 'This address was not found. Open / to enter a Yandex.Disk link.' });
  });
  app.use((error, req, res, next) => {
    if (res.headersSent) return next(error);
    const status = error instanceof HttpError ? error.status : error instanceof URIError ? 400 : 500;
    if (status === 500) console.error('Unexpected request error:', error);
    res.status(status).set('Cache-Control', 'no-store').json({
      error: error.code ?? (status === 400 ? 'INVALID_PATH' : 'INTERNAL_ERROR'),
      message: error instanceof HttpError ? error.message : status === 400 ? 'The URL contains invalid encoding.' : 'An unexpected error occurred. Please try again.',
    });
  });
  return app;
}
