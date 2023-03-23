# Bu Araç @keyiflerolsun tarafından | @KekikAkademi için yazılmıştır.

from flask         import Flask, jsonify, request, redirect, send_file, Response
from flask_caching import Cache
from requests      import get
from re            import compile, search
from io            import BytesIO

app   = Flask(__name__)
cache = Cache(config={"CACHE_TYPE": "FileSystemCache", "CACHE_DIR": "__FlaskCache__", "CACHE_DEFAULT_TIMEOUT": 24 * 60 * 60})
cache.init_app(app)

@app.route("/")
@cache.cached()
async def home():
    return jsonify({"data": "Hello, World!"})

@app.route("/redirect")
@cache.cached(query_string=True)
async def redirect_fnc():
    _path = request.args.get("path")
    if not _path:
        return jsonify({"error": "/redirect?path=i/3h1ud7JlhIxAXA"}), 406

    pk_split = search(compile(r"\/([a-z])\/(\w+)(\/.*)?"), f"/{_path}")
    if not pk_split:
        return jsonify({"error": "Invalid URL format."}), 404

    pk_type = pk_split.group(1)
    pk_hash = pk_split.group(2)
    pk_path = f"&path={pk_split.group(3)}" if pk_split.group(3) else ""

    public_key   = f"https://disk.yandex.ru/{pk_type}/{pk_hash}{pk_path}"
    yandex_istek = get(f"https://cloud-api.yandex.net/v1/disk/public/resources/download?public_key={public_key}")
    redirect_url = yandex_istek.json()["href"]

    return jsonify({"data": redirect_url})

@app.route("/<path:path>")
async def get_file(path):
    try:
        veri         = get(f"http://localhost:5000/redirect?path={path}").json()
        redirect_url = veri.get("data")
        if not redirect_url:
            return jsonify({"error": "Download Limit Exceeded!"}), 401

        # ! Redirect » Örneği
        return redirect(redirect_url, code=302)

        # ! send_file » Örneği
        # istek = get(redirect_url, allow_redirects=True)
        # return send_file(BytesIO(istek.content), mimetype=istek.headers["Content-Type"], as_attachment=False)

        # ! Stream » Örneği
        # def generate():
        #     r = get(redirect_url, stream=True)
        #     yield from r.iter_content(chunk_size=1024)

        # return Response(generate(), headers={
        #     "Content-Disposition" : "inline",
        #     "Content-Type"        : "video/mp4"
        # })

    except Exception as hata:
        return jsonify({"error": f"{type(hata).__name__} » {hata}"}), 500

if __name__ == "__main__":
    app.run(debug=False, host="::", port=5000, threaded=True)