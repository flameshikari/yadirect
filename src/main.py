from json import loads
from re import compile, search
from urllib.parse import urlparse
from urllib.request import urlopen
from http.server import HTTPServer, BaseHTTPRequestHandler
from socketserver  import ThreadingMixIn


with open('index.html', 'r') as file:
    data = file.read()


def redirect(self):
    pattern = compile('\/([a-z])\/(\w+)(\/.*)?')
    pk_split = search(pattern, urlparse(self.path).path)
    pk_type = pk_split.group(1)
    pk_hash = pk_split.group(2)
    api = 'https://cloud-api.yandex.net/v1/disk/public/resources/download?public_key={}'
    pk_path = f'&path={pk_split.group(3)}' if pk_split.group(3) else ''
    public_key = f'https://disk.yandex.ru/{pk_type}/{pk_hash}{pk_path}'
    response = urlopen(api.format(public_key)).read().decode('utf-8')
    redirect = loads(response)['href']
    self.send_response(301)
    self.send_header('Location', redirect)
    self.end_headers()


class Server(BaseHTTPRequestHandler):

    def do_HEAD(self):
        try:
            redirect(self)
            self.send_header('Content-type', 'text/html')
            self.end_headers()
            self.wfile.write(bytes(data, 'utf-8'))
        except:
            if self.path == '/': self.send_response(200)
            else: self.send_response(404)
            self.send_header('Content-type', 'text/html')
            self.end_headers()

    def do_GET(self):
        try: redirect(self)
        except:
            if self.path == '/': self.send_response(200)
            else: self.send_response(404)
            self.send_header('Content-type', 'text/html')
            self.end_headers()
            self.wfile.write(bytes(data, 'utf-8'))


class ThreadingSimpleServer(ThreadingMixIn, HTTPServer):
    pass


def run():
    server_address = ('0.0.0.0', 80)
    httpd = ThreadingSimpleServer(server_address, Server)
    httpd.serve_forever()


if __name__ == '__main__': run()
