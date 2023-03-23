# Usage

For example, we have a next shared URL: https://disk.yandex.ru/d/0gRKgtGjqWil4g

1. Run `python main.py`

2. Append the shared URL (or the part of it after the domain) and you can download the file/zipped dir from these URLs

- http://localhost:5000/d/0gRKgtGjqWil4g
- http://localhost:5000/https://disk.yandex.ru/d/0gRKgtGjqWil4g

3. The file/zipped dir will start downloading

It's better to host it on the server with 'disk' subdomain, like `disk.example.com`, so you can just change domain from `disk.yandex.ru` to `disk.example.com` and get a direct download link.

Check examples below with my self-hosted server!

# Examples

- https://disk.yandex.ru/d/0gRKgtGjqWil4g → https://disk.hexed.pw/d/0gRKgtGjqWil4g
- https://disk.yandex.ru/d/YHflGF3zn3vf3w/DriveDroid.img → https://disk.hexed.pw/d/YHflGF3zn3vf3w/DriveDroid.img