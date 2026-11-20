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

// beaconActive 记录信标当前是否生效，仅在状态翻转时输出提示。
var beaconActive bool

// beaconProbeSince 记录本次寻找信标的起始时间；模组注入模式下长时间找不到
// 信标时提示用户可能尚未安装信标模组。beaconMissingWarned 保证只提醒一次。
var (
	beaconProbeSince    time.Time
	beaconMissingWarned bool
)

// 信标事件识别状态：用于输出与内存探测路径同等级、同措辞的
// 大传送 / 进出神庙 / 庙内静止日志。
var (
	beaconLastPos    [3]float32
	beaconHasLast    bool
	beaconLastState  uint32
	beaconStateKnown bool
	beaconLastMove   time.Time
	beaconStillTold  bool
)

// beaconEvent 识别信标侧的状态变化并按内存探测路径的等级输出：
// 大传送 → info、进出神庙 → info、庙内静止保持 → info。
func beaconEvent(b Beacon) {
	pos := [3]float32{b.X, b.Y, b.Z}
	if beaconHasLast {
		if d := dist3(beaconLastPos, pos); d > 300.0 {
			dest := formatDestName(pos)
			logInfo("Eden 信标检测到大传送 (距离=%.1fm) -> %s (%.1f, %.1f, %.1f) [直接跟随] / Beacon fast travel (dist=%.1fm) -> %s [Direct takeover]",
				d, dest, pos[0], pos[1], pos[2], d, dest)
			beaconStillTold = false
		}
	}
	if beaconHasLast && dist3(beaconLastPos, pos) > 0.05 {
		beaconLastMove = time.Now()
		beaconStillTold = false
	}
	beaconLastPos = pos
	beaconHasLast = true

	inShrine := b.State == stShrine || b.State == stTrial
	if !beaconStateKnown {
		beaconStateKnown = true
		beaconLastState = b.State
		return
	}
	if b.State != beaconLastState {
		prev := beaconLastState
		beaconLastState = b.State
		beaconStillTold = false
		switch {
		case inShrine:
			logInfo("Eden 信标：角色进入试炼场 -> 锚定神庙入口 (%.1f, %.1f, %.1f) / Beacon: entered shrine -> holding entrance",
				pos[0], pos[1], pos[2])
		case prev == stShrine || prev == stTrial:
			logInfo("Eden 信标：角色离开试炼场 -> 恢复大世界追踪 (%.1f, %.1f, %.1f) / Beacon: left shrine -> resumed world tracking",
				pos[0], pos[1], pos[2])
		}
		return
	}
	if !inShrine {
		beaconLastMove = time.Time{}
		beaconStillTold = false
		return
	}
	if beaconLastMove.IsZero() {
		beaconLastMove = time.Now()
		return
	}
	if !beaconStillTold && time.Since(beaconLastMove) > 8*time.Second {
		beaconStillTold = true
		logInfo("Eden 信标：庙内位置静止 (%.1f, %.1f, %.1f)，保持庙内坐标 / Beacon: standing still in shrine, holding local coords",
			pos[0], pos[1], pos[2])
	}
}

// tryBeaconTiers 尝试 tier1(RAM) 与 tier2(pos.json)。命中即发布统一状态并
// 返回 true（调用方 continue，跳过扫描器）；均未命中返回 false（落 tier3）。
// tier1/2 数据权威，verified=true，跳过扫描器运动确认。
//
// gameReady 表示游戏已在模拟器内加载：只有游戏确实在跑、却始终找不到信标时
// 才提示"未安装模组"，避免启动阶段游戏尚未加载完被误判。
func tryBeaconTiers(h uintptr, p *edenPlatform, gameReady bool) bool {
	if b, ok := beacon.read(h, p); ok {
		setLock(beacon.hostAddr, true, 1, "beacon")
		publishState(true, b.X, b.Y, b.Z, true, 1, "beacon")
		beaconMissingWarned = false
		if !beaconActive {
			beaconActive = true
			logOK("Eden 信标定位已生效，坐标来自游戏内插件 / Beacon positioning active")
		}
		beaconEvent(b)
		return true
	}
	dir := edenSdmcXnaviDir(p)
	if b, _, ok := readPosJSON(dir); ok {
		// 新鲜度用宿主侧文件 mtime（宿主时钟权威，不依赖插件时钟）。
		if fi, err := os.Stat(filepath.Join(dir, "pos.json")); err == nil &&
			time.Since(fi.ModTime()) <= beaconStaleAfter && beaconCoordOK(b) {
			setLock(0, true, 1, "beacon-json")
			publishState(true, b.X, b.Y, b.Z, true, 1, "beacon-json")
			beaconMissingWarned = false
			if !beaconActive {
				beaconActive = true
				logOK("Eden 信标定位已生效（文件通道）/ Beacon positioning active (file channel)")
			}
			beaconEvent(b)
			return true
		}
	}
	if beaconActive {
		beaconActive = false
		logWarn("Eden 信标不可用，回退内存探测 / Beacon unavailable, falling back to memory scan")
	}
	if !gameReady {
		// 游戏尚未加载完：不判定"模组缺失"，重置计时等待游戏就绪。
		beaconProbeSince = time.Time{}
		beaconMissingWarned = false
		return false
	}
	if beaconProbeSince.IsZero() {
		beaconProbeSince = time.Now()
	}
	if locateMode == "mod-inject" && !beaconMissingWarned &&
		time.Since(beaconProbeSince) > 20*time.Second {
		beaconMissingWarned = true
		logWarn("模组注入模式：游戏已运行但未找到信标模组，请安装 Xnavi-Beacon 模组并重启游戏；当前回退内存探测 / Beacon mod not found, falling back to memory scan")
	}
	return false
}
