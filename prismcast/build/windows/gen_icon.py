"""从 internal/dlna/icon.png 生成 build/windows/icon.ico（供 go:embed 与 wails build 使用）"""
from PIL import Image
import struct
import io
import os

root = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
src = os.path.join(root, "internal", "dlna", "icon.png")
output = os.path.join(os.path.dirname(os.path.abspath(__file__)), "icon.ico")

img = Image.open(src).convert("RGBA")
sizes = [16, 32, 48, 64, 128, 256]
icon_dir = struct.pack("<HHH", 0, 1, len(sizes))
entries, data = b"", b""
offset = 6 + 16 * len(sizes)
for s in sizes:
    resized = img.resize((s, s), Image.LANCZOS)
    buf = io.BytesIO()
    resized.save(buf, format="PNG")
    d = buf.getvalue()
    wb = s if s < 256 else 0
    entries += struct.pack("<BBBBHHII", wb, wb, 0, 0, 1, 32, len(d), offset)
    data += d
    offset += len(d)
with open(output, "wb") as f:
    f.write(icon_dir + entries + data)
print(f"ICO: {os.path.getsize(output)} bytes -> {output}")
