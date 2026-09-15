"""Capture the BOTW overlay window via PrintWindow into a PNG."""
import ctypes
from ctypes import wintypes
import struct

u32 = ctypes.windll.user32
gdi32 = ctypes.windll.gdi32

hWnd = None
def proc(hwnd, _):
    global hWnd
    ln = u32.GetWindowTextLengthW(hwnd)
    if ln <= 0:
        return True
    buf = ctypes.create_unicode_buffer(ln + 2)
    u32.GetWindowTextW(hwnd, buf, ln + 2)
    if buf.value == "BOTW mini-map":
        hWnd = hwnd
        return False
    return True
u32.EnumWindows(ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)(proc), 0)
if not hWnd:
    print("overlay window not found")
    raise SystemExit(1)

r = wintypes.RECT()
u32.GetWindowRect(hWnd, ctypes.byref(r))
w, h = r.right - r.left, r.bottom - r.top
print("window", w, "x", h)

# screen DC -> memory DC
hdc = u32.GetWindowDC(hWnd)
mem = gdi32.CreateCompatibleDC(hdc)
bmp = gdi32.CreateCompatibleBitmap(hdc, w, h)
old = gdi32.SelectObject(mem, bmp)
ok = u32.PrintWindow(hWnd, mem, 2)   # PW_RENDERFULLCONTENT
print("printwindow:", ok)

# save bitmap as PNG via PIL
from PIL import Image
import ctypes as c
BITMAPINFOHEADER = struct.Struct("<iiiHHiiiiii")
bih = BITMAPINFOHEADER.pack(40, w, h, 1, 24, 0, 0, 0, 0, 0, 0)
buf = ctypes.create_string_buffer(w * h * 4)
gdi32.GetDIBits(mem, bmp, 0, h, buf, ctypes.cast(ctypes.create_string_buffer(bih), ctypes.POINTER(ctypes.c_byte)), 0)
img = Image.frombuffer("RGB", (w, h), buf, "raw", "BGRX", 0, 1)
img = img.transpose(Image.FLIP_TOP_BOTTOM)
out = r"E:\WorkSpace\BOTWmap\live\_research\overlay_capture.png"
img.save(out)
print("saved", out)

gdi32.SelectObject(mem, old)
gdi32.DeleteObject(bmp)
gdi32.DeleteDC(mem)
u32.ReleaseDC(hWnd, hdc)
