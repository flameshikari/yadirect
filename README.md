![Yadirect](./.github/assets/opengraph.png)

# 🔗 [Yadirect](https://github.com/flameshikari/yadirect) [![Build Status](https://img.shields.io/github/actions/workflow/status/flameshikari/yadirect/build.yml)](https://github.com/flameshikari/yadirect/actions)

## ℹ️ Description

The service allows you to receive direct links to download a file/folder from [Yandex.Disk](https://disk.yandex.ru).

The idea is hosting this service on `disk` subdomain with any reverse proxy server. For example:
- you have this service accessible at [`disk.hexed.pw`](https://disk.hexed.pw)
- you have non-direct link [`disk.yandex.ru/d/0gRKgtGjqWil4g`](https://disk.yandex.ru/d/0gRKgtGjqWil4g)

All you had to do is replace `yandex.ru` to `hexed.pw` (will be [`disk.hexed.pw/d/0gRKgtGjqWil4g`](https://disk.hexed.pw/d/0gRKgtGjqWil4g)), then after pressing Enter the file/folder will start the downloading immediatly.

By the way the service has the frontend just in case.

## 🐳 Installation

Available in <a href="https://github.com/flameshikari/yadirect/pkgs/container/yadirect">GHCR</a> and <a href="https://hub.docker.com/r/flameshikari/yadirect">Docker Hub</a> for multiple platforms.

```yaml
services:
  yadirect:
    image: flameshikari/yadirect
    container_name: yadirect
    ports:
      - 3000:3000
```
