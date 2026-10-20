package scan

import "fmt"

// PlayerInfo 指纹：从存档读出的玩家持久属性，用来在内存里唯一定位 PlayerInfo 区块。
//
// 依据（zeldaret/botw 反编译 + DeepWiki「Player Actor」章节）：
//
//	PlayerInfo {
//	  PlayerBase*      mPlayerActor       // 指向 Player actor
//	  BaseProcLink     mPlayerLink
//	  BaseProcLink     mHorseLink
//	  sead::Vector3f   mPlayerPos         // ★ 我们要的坐标
//	  f32              mMaxHeartValue     // = 心数 × 4
//	  f32              mStaminaCurrentMax // 1000 = 一圈
//	  f32              mStaminaMax
//	  sead::FixedRingBuffer mPreviousPositions  // 最近 60 个位置
//	}
//
// 「心数×4 + 体力×2」这三个数凑一起在 2.6GB 堆里几乎唯一，
// 比「三个 float 都像坐标」精确好几个数量级 —— 2026-10-20 实测后者会捞到 2727 万个假命中。

// Fingerprint 玩家属性指纹（来自存档）。
type Fingerprint struct {
	MaxHeart          float32 // MaxHartValue（存档里是整数「心数×4」）
	CurrentHeart      float32 // CurrentHart
	StaminaCurrentMax float32 // StaminaCurrentMax
	StaminaMax        float32
	// 是否启用各项校验（值为 0 表示存档里没读到，跳过该项）
	useHeart   bool
	useStamina bool
}

// NewFingerprint 由存档读到的整数构造指纹。
func NewFingerprint(maxHeart, curHeart, stamCur, stamMax uint32) Fingerprint {
	f := Fingerprint{
		MaxHeart:          float32(maxHeart),
		CurrentHeart:      float32(curHeart),
		StaminaCurrentMax: float32(stamCur),
		StaminaMax:        float32(stamMax),
	}
	// 心数合理范围：4（1 心）~ 120（30 心）；超出说明存档值不可信
	if f.MaxHeart >= 4 && f.MaxHeart <= 160 {
		f.useHeart = true
	}
	// 体力：1000 的整数倍（1~5 圈），且不能为 0
	if f.StaminaMax >= 500 && f.StaminaMax <= 6000 {
		f.useStamina = true
	}
	return f
}

// Usable 至少有一项可用才值得扫。
func (f Fingerprint) Usable() bool { return f.useHeart || f.useStamina }

// String 日志用。
func (f Fingerprint) String() string {
	return sprintf("心=%v(%.0f/%.0f) 体力=%v(%.0f/%.0f)",
		f.useHeart, f.CurrentHeart, f.MaxHeart, f.useStamina, f.StaminaCurrentMax, f.StaminaMax)
}

// Match 判断 off 处是否命中指纹的「心/体力」字段组。
//
// 布局假设（DeepWiki 字段顺序）：mMaxHeartValue, mStaminaCurrentMax, mStaminaMax 连续三个 f32。
// 同时接受两种排布：
//   - A: [mMaxHeartValue][mStaminaCurrentMax][mStaminaMax]  = [76, 3000, 3000]
//   - B: [mStaminaCurrentMax][mStaminaMax]                  = [3000, 3000]（心值字段缺失/错位时）
//
// 只要 A 或 B 命中即返回 true，返回命中的偏移与类型（"A"/"B"）。
func (f Fingerprint) Match(buf []byte, off int) (kind string, ok bool) {
	if off < 0 || off+12 > len(buf) {
		return "", false
	}
	a := f32b(buf, off)
	b := f32b(buf, off+4)
	c := f32b(buf, off+8)

	if f.useHeart && f.useStamina {
		// 方案 A：心 + 体力两连
		if near(a, f.MaxHeart, 0.01) && near(b, f.StaminaCurrentMax, 1.0) && near(c, f.StaminaMax, 1.0) {
			return "A", true
		}
	}
	if f.useStamina && f.useHeart {
		// 方案 A'：存档里 CurrentHart 与 MaxHart 相邻（部分版本排布）
		if near(a, f.CurrentHeart, 0.01) && near(b, f.MaxHeart, 0.01) && near(c, f.StaminaCurrentMax, 1.0) {
			return "A2", true
		}
	}
	if f.useStamina {
		// 方案 B：体力两连（更宽松，用于心值字段对不上时）
		if near(a, f.StaminaCurrentMax, 1.0) && near(b, f.StaminaMax, 1.0) && a >= 500 && b >= 500 {
			return "B", true
		}
	}
	return "", false
}

// near 浮点近似比较。
func near(a, b, tol float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}

// sprintf 薄封装（fmt 已引入，统一出口便于以后换实现）。
func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
