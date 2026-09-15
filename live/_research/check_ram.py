# -*- coding: utf-8 -*-
"""检查 Ryujinx 进程与 guest DRAM 块(BOTW 是否已加载)"""
import ctypes
from ctypes import wintypes

PROCESS_QUERY_INFORMATION = 0x0400
PROCESS_VM_READ = 0x0010
MEM_COMMIT = 0x1000
MEM_MAPPED = 0x40000
PAGE_GUARD = 0x100

class MEMORY_BASIC_INFORMATION(ctypes.Structure):
    _fields_ = [
        ("BaseAddress", ctypes.c_void_p),
        ("AllocationBase", ctypes.c_void_p),
        ("AllocationProtect", wintypes.DWORD),
        ("PartitionId", wintypes.WORD),
        ("RegionSize", ctypes.c_size_t),
        ("State", wintypes.DWORD),
        ("Protect", wintypes.DWORD),
        ("Type", wintypes.DWORD),
    ]

class PROCESSENTRY32(ctypes.Structure):
    _fields_ = [("dwSize", wintypes.DWORD), ("cntUsage", wintypes.DWORD),
                ("th32ProcessID", wintypes.DWORD),
                ("th32DefaultHeapID", ctypes.POINTER(ctypes.c_ulong)),
                ("th32ModuleID", wintypes.DWORD), ("cntThreads", wintypes.DWORD),
                ("th32ParentProcessID", wintypes.DWORD),
                ("pcPriClassBase", ctypes.c_long), ("dwFlags", wintypes.DWORD),
                ("szExeFile", ctypes.c_char * 260)]

k32 = ctypes.windll.kernel32
k32.OpenProcess.restype = wintypes.HANDLE

snap = k32.CreateToolhelp32Snapshot(0x2, 0)
e = PROCESSENTRY32(); e.dwSize = ctypes.sizeof(PROCESSENTRY32)
pid = None
ok = k32.Process32First(snap, ctypes.byref(e))
while ok:
    if e.szExeFile.decode('ascii', 'replace').lower().startswith('ryujinx'):
        pid = e.th32ProcessID; break
    ok = k32.Process32Next(snap, ctypes.byref(e))
k32.CloseHandle(snap)
print('Ryujinx pid:', pid)
if not pid:
    raise SystemExit()

h = k32.OpenProcess(PROCESS_QUERY_INFORMATION | PROCESS_VM_READ, False, pid)
mbi = MEMORY_BASIC_INFORMATION()
addr, regions = 0, []
while addr < 0x7FFFFFFFFFFF:
    n = k32.VirtualQueryEx(h, ctypes.c_void_p(addr), ctypes.byref(mbi), ctypes.sizeof(mbi))
    if n == 0:
        break
    base, size = mbi.BaseAddress or 0, mbi.RegionSize
    if mbi.State == MEM_COMMIT:
        regions.append((base, size, mbi.Type, mbi.Protect & 0xFF))
    nxt = base + size
    addr = nxt if nxt > addr else addr + 0x1000

regions.sort(key=lambda r: -r[1])
print('top regions:')
for b, s, t, p in regions[:8]:
    print('  0x%012X  %8.0f MB  type=0x%X prot=0x%X' % (b, s / 1048576.0, t, p))
