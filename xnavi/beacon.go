// beacon.go — Xnavi 信标读取层。
//
// 协议见 xnavi-beacon/beacon.h。插件把 64 字节信标写入静态内存缓冲，
// 本文件按魔数扫描定位信标、缓存地址后直读，并做解析与校验
//（魔数/版本/CRC/序号判活/按状态选择坐标校验器）。
// 信标不可用时由 watch_eden.go 回退到原有内存扫描。

package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"math"
	"os"
	"path/filepath"
	"time"
	"unsafe"
)

const (
	beaconMagic      = 0x56414E58 // "XNAV" 小端
	beaconSchema     = 1
	beaconSize       = 64
	beaconCRCLen     = 44 // CRC 覆盖 [0,44)
	beaconStaleAfter = 3 * time.Second
	beaconJSONPeriod = 5 * time.Second // beacon.json 周期重读
)

// 声明式游戏状态（与 beacon.h enum xnav_state 一致）。
const (
	stOverworld = 0
	stShrine    = 1
	stTrial     = 2
	stMenu      = 3
	stLoading   = 4
	stDead      = 5
	stMemory    = 6
)

// Beacon 是 64B 信标的宿主镜像（逐字段对齐 beacon.h）。
type Beacon struct {
	Schema       uint16
	Build        uint16
	Seq          uint32
	X, Y, Z      float32
	Yaw          float32
	State        uint32
	GameClock    uint64
	ActorPtrHash uint32
	HP, Stamina  float32
	Flags        uint32
}

// ParseBeacon 解析并校验 64B 信标：长度/magic/schema/CRC。
// 不做坐标与 seq 判活（由调用方按上下文判定）。
func ParseBeacon(b []byte) (Beacon, bool) {
	if len(b) < beaconSize {
		return Beacon{}, false
	}
	if binary.LittleEndian.Uint32(b[0:4]) != beaconMagic {
		return Beacon{}, false
	}
	schema := binary.LittleEndian.Uint16(b[4:6])
	if schema != beaconSchema {
		return Beacon{}, false
	}
	if crc32.ChecksumIEEE(b[0:beaconCRCLen]) != binary.LittleEndian.Uint32(b[44:48]) {
		return Beacon{}, false
	}
	var o Beacon
	o.Schema = schema
	o.Build = binary.LittleEndian.Uint16(b[6:8])
	o.Seq = binary.LittleEndian.Uint32(b[8:12])
	o.X = math.Float32frombits(binary.LittleEndian.Uint32(b[12:16]))
	o.Y = math.Float32frombits(binary.LittleEndian.Uint32(b[16:20]))
	o.Z = math.Float32frombits(binary.LittleEndian.Uint32(b[20:24]))
	o.Yaw = math.Float32frombits(binary.LittleEndian.Uint32(b[24:28]))
	o.State = binary.LittleEndian.Uint32(b[28:32])
	o.GameClock = binary.LittleEndian.Uint64(b[32:40])
	o.ActorPtrHash = binary.LittleEndian.Uint32(b[40:44])
	o.HP = math.Float32frombits(binary.LittleEndian.Uint32(b[48:52]))
	o.Stamina = math.Float32frombits(binary.LittleEndian.Uint32(b[52:56]))
	o.Flags = binary.LittleEndian.Uint32(b[56:60])
	return o, true
}

// beaconCoordOK 按声明式 state 选坐标校验器：神庙/试炼用本地尺度，
// 其余用世界尺度。声明式 state 直接决定校验，消灭进庙/站桩启发式。
func beaconCoordOK(b Beacon) bool {
	if b.State == stShrine || b.State == stTrial {
		return ShrineCoordOK(b.X, b.Y, b.Z)
	}
	return WorldCoordOK(b.X, b.Y, b.Z)
}

// beaconRAM 缓存 RAM 信标读取所需的锚点/地址与 seq 判活状态。
// 游戏重启会重映射堆，magic 失败或 pid 变化时重推导（自愈）。
type beaconRAM struct {
	pid           uint32
	arenaLow      uintptr
	hostAddr      uintptr
	guestVA       uint64
	haveAddr      bool
	lastResolve   time.Time
	lastSeq       uint32
	lastSeqChange time.Time
	seqInit       bool
}

// resolve 经魔数扫描发现信标 hostAddr（插件写静态 .bss，无 fs/beacon.json）。
// 带缓存：已有地址且 magic 仍有效则复用；失效或周期到期时重扫（节流）。
func (br *beaconRAM) resolve(h uintptr, p *edenPlatform, force bool) bool {
	if br.haveAddr && br.pid == p.PID() {
		if buf := readMem(h, br.hostAddr, beaconSize); len(buf) >= beaconSize {
			if _, ok := ParseBeacon(buf); ok {
				return true
			}
		}
		br.haveAddr = false
	}
	if !force && time.Since(br.lastResolve) < beaconJSONPeriod {
		return false
	}
	br.lastResolve = time.Now()
	addr := beaconDiscover(h)
	if addr == 0 {
		br.haveAddr = false
		return false
	}
	br.hostAddr = addr
	br.pid = p.PID()
	br.haveAddr = true
	return true
}

// beaconDiscover 扫描进程已提交 RW 区寻找信标魔数+合法结构，返回 hostAddr。
// 信标在插件 .bss（小区域）；跳过超大堆区以控成本。发现后由 resolve 缓存。
func beaconDiscover(h uintptr) uintptr {
	if h == 0 {
		return 0
	}
	const limit = uintptr(0x7FFFFFFFFFFF)
	magic := []byte{0x58, 0x4E, 0x41, 0x56} // "XNAV" 小端
	addr := uintptr(0)
	for addr < limit {
		var mbi MemoryBasicInformation
		rr, _, _ := procVirtualQueryEx.Call(h, addr, uintptr(unsafe.Pointer(&mbi)), unsafe.Sizeof(mbi))
		if rr == 0 {
			break
		}
		b, sz := mbi.BaseAddress, mbi.RegionSize
		pr := mbi.Protect & 0xFF
		isRW := (pr == 0x04 || pr == 0x40) && (mbi.Protect&pageGuard == 0)
		if mbi.State == memCommit && isRW && sz >= beaconSize && sz <= 64<<20 {
			for off := uintptr(0); off+beaconSize <= sz; off += 1 << 20 {
				n := uintptr(1 << 20)
				if off+n > sz {
					n = sz - off
				}
				buf := readMem(h, b+off, int(n))
				for i := 0; i+beaconSize <= len(buf); {
					idx := bytes.Index(buf[i:], magic)
					if idx < 0 {
						break
					}
					pos := i + idx
					if pos+beaconSize <= len(buf) {
						if _, ok := ParseBeacon(buf[pos : pos+beaconSize]); ok {
							return b + off + uintptr(pos)
						}
					}
					i = pos + 4
				}
			}
		}
		nxt := b + sz
		if nxt > addr {
			addr = nxt
		} else {
			addr += 0x1000
		}
	}
	return 0
}

// read 读 RAM 信标并判活。返回 (信标, 是否新鲜可用)。
// seq 冻结 >beaconStaleAfter 视为插件停摆 → 不可用（落 tier2/3）。
func (br *beaconRAM) read(h uintptr, p *edenPlatform) (Beacon, bool) {
	if !br.resolve(h, p, false) {
		return Beacon{}, false
	}
	buf := readMem(h, br.hostAddr, beaconSize)
	b, ok := ParseBeacon(buf)
	if !ok {
		// magic/CRC 失败：可能游戏重启重映射 → 下次强制重推导
		br.haveAddr = false
		return Beacon{}, false
	}
	now := time.Now()
	if !br.seqInit || b.Seq != br.lastSeq {
		br.lastSeq = b.Seq
		br.lastSeqChange = now
		br.seqInit = true
	} else if now.Sub(br.lastSeqChange) > beaconStaleAfter {
		return Beacon{}, false // 插件停摆
	}
	if !beaconCoordOK(b) {
		return Beacon{}, false
	}
	return b, true
}

// 包级信标读取器。
var beacon = beaconRAM{}

// tryBeaconTiers 尝试 tier1(RAM) 与 tier2(pos.json)。命中即发布统一状态并
// 返回 true（调用方 continue，跳过扫描器）；均未命中返回 false（落 tier3）。
// tier1/2 数据权威，verified=true，跳过扫描器运动确认。
func tryBeaconTiers(h uintptr, p *edenPlatform) bool {
	if b, ok := beacon.read(h, p); ok {
		setLock(beacon.hostAddr, true, 1, "beacon")
		publishState(true, b.X, b.Y, b.Z, true, 1, "beacon")
		return true
	}
	dir := edenSdmcXnaviDir(p)
	if b, _, ok := readPosJSON(dir); ok {
		// 新鲜度用宿主侧文件 mtime（宿主时钟权威，不依赖插件时钟）。
		if fi, err := os.Stat(filepath.Join(dir, "pos.json")); err == nil &&
			time.Since(fi.ModTime()) <= beaconStaleAfter && beaconCoordOK(b) {
			setLock(0, true, 1, "beacon-json")
			publishState(true, b.X, b.Y, b.Z, true, 1, "beacon-json")
			return true
		}
	}
	return false
}
