import io, struct, sys
from PIL import Image, ImageDraw
def draw(n):
    im = Image.new("RGBA", (n, n), (0, 0, 0, 0)); d = ImageDraw.Draw(im)
    m = n * 0.09; d.rounded_rectangle([m, m, n - m, n - m], radius=n * 0.2, fill=(14, 14, 16, 255))
    w = n * 0.075; cx = n / 2
    d.line([(cx, n * 0.26), (cx, n * 0.6)], fill="white", width=int(w))
    d.line([(n * 0.34, n * 0.46), (cx, n * 0.62), (n * 0.66, n * 0.46)], fill="white", width=int(w), joint="curve")
    d.line([(n * 0.3, n * 0.74), (n * 0.7, n * 0.74)], fill=(140, 140, 140, 255), width=int(w))
    return im
chunks = b""
for tag, size in [(b"ic07", 128), (b"ic08", 256), (b"ic09", 512), (b"ic10", 1024)]:
    buf = io.BytesIO(); draw(size).save(buf, "PNG"); data = buf.getvalue()
    chunks += tag + struct.pack(">I", len(data) + 8) + data
open(sys.argv[1], "wb").write(b"icns" + struct.pack(">I", len(chunks) + 8) + chunks)
