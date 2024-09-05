# Usage

Imagine we have a Yandex.Disk link `https://disk.yandex.ru/d/0gRKgtGjqWil4g` and this service serving at `localhost:3000`. You need to cut off `https://disk.yandex.ru` from URL and append it to `localhost:3000`. Final URL will be `localhost:3000/d/0gRKgtGjqWil4g`

It's better to host it on the server with `disk` subdomain, like `disk.example.com`, so you can just change domain from `disk.yandex.ru` to `disk.example.com` and this service will redirect to file downloading on execution.
