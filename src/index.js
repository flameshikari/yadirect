#!/usr/bin/env node

import apicache from 'apicache'
import cors from 'cors'
import express from 'express'
import fetch from 'node-fetch'
import helmet from 'helmet'


const app = express()
const router = express.Router()
const port = 3000
const cache = apicache.middleware


router.get('/[di]/*', async (req, res, next) => {
  const regexp = /([di])\/(\w+)\/?(.*)?/
  try {
    const match = req.originalUrl.match(regexp)
    const type = match[1]
    const hash = match[2]
    const path = match[3] ? `path=/${match[3]}&` : ''
    const publicKeyUrl = `https://disk.yandex.ru/${type}/${hash}`
    const redirectUrl = `https://cloud-api.yandex.net/v1/disk/public/resources/download?${path}public_key=${publicKeyUrl}`
    const response = await fetch(redirectUrl)
    const result = await response.json()
    res.redirect(result.href)
  } catch(error) {
    next()
  }
})

app.set('json spaces', 2)
   .set('x-powered-by', false)

app.use(
  cache('1 hour'),
  express.urlencoded({ extended: false }),
  express.json(),
  cors(),
  helmet({ contentSecurityPolicy: false, xDownloadOptions: false }),
  router,
  express.static('public')
)

app.listen(port, () => {
  console.log(`nodejs: server: listening at ${port}/tcp`)
})