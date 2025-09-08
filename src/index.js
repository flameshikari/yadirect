#!/usr/bin/env node

import cors from 'cors';
import express from 'express';
import helmet from 'helmet';

import NodeFetchCache, { MemoryCache } from 'node-fetch-cache';


const app = express();
const router = express.Router();
const port = 3000;
const fetch = NodeFetchCache.create({
    cache: new MemoryCache({ ttl: 60000 }),
    shouldCacheResponse: (response) => response.ok,
});

router.get(/\/list\/([di])\/(\w+)\/?(.*)?/, async (req, res, next) => {
  try {
    const regexp = /\/list\/([di])\/(\w+)\/?(.*)?/;
    const match = req.originalUrl.match(regexp);
    const type = match[1];
    const hash = match[2];
    const folderPath = match[3] ? `path=/${match[3]}&` : '';
    const publicKeyUrl = `https://disk.yandex.ru/${type}/${hash}`;
    const apiUrl = `https://cloud-api.yandex.net/v1/disk/public/resources?${folderPath}public_key=${encodeURIComponent(publicKeyUrl)}`;
  
    const response = await fetch(apiUrl);
    const data = await response.json();

    if (response.ok && data._embedded && data._embedded.items) {
      res.json(data._embedded.items);
    } else {
      res.status(response.status).json(data);
    }
  } catch (e) {
    res.status(500).json({ error: e.message });
  }
});

router.get(/([di])\/(\w+)\/?(.*)?/, async (req, res, next) => {
    try {
        const regexp = /([di])\/(\w+)\/?(.*)?/;
        const match = req.originalUrl.match(regexp);
        const type = match[1];
        const hash = match[2];
        const path = match[3] ? `path=/${match[3]}&` : '';
        const publicKeyUrl = `https://disk.yandex.ru/${type}/${hash}`;
        const redirectUrl = `https://cloud-api.yandex.net/v1/disk/public/resources/download?${path}public_key=${publicKeyUrl}`;
        const response = await fetch(redirectUrl);
        const result = await response.json();
        if (response.ok) {
            res.redirect(result.href);
        } else {
            res.json(result);
        };
    } catch(e) {
        res.redirect('/');
    };
});

router.all('*', (req, res) => {
    res.redirect('/');
});

app.set('json spaces', 2);
app.set('x-powered-by', false);

app.use(
    cors(),
    helmet({ contentSecurityPolicy: false, xDownloadOptions: false }),
    express.urlencoded({ extended: false }),
    express.json(),
    express.static('./public'),
    router,
);


app.listen(port, () => {
    console.log(`listening at ${port}/tcp`);
});
