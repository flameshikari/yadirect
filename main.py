from json import loads
from re import compile, search
from urllib.parse import urlparse
from http.server import HTTPServer, BaseHTTPRequestHandler

from requests import get


class Server(BaseHTTPRequestHandler):
    def do_GET(self):
        try:
            api = 'https://cloud-api.yandex.net/v1/disk/public/resources/download?public_key={}'
            pattern = compile('\/([a-z])\/(\w+)(\/.*)?')

            pk_split = search(pattern, urlparse(self.path).path)

            pk_type = pk_split.group(1)
            pk_hash = pk_split.group(2)
            pk_path = f'&path={pk_split.group(3)}' if pk_split.group(3) else ''

            public_key = f'https://disk.yandex.ru/{pk_type}/{pk_hash}{pk_path}'

            response = get(api.format(public_key))
            redirect = loads(response.text)['href']

            self.send_response(301)
            self.send_header('Location', redirect)
            self.end_headers()

        except: pass


def run(server_class=HTTPServer, handler_class=Server, port=8080):
    server_address = ('0.0.0.0', port)
    httpd = server_class(server_address, handler_class)
    httpd.serve_forever()


if __name__ == '__main__': run()
