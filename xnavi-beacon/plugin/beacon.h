/* beacon.h — Xnavi 信标协议 v1（guest/host 单一契约）
 *
 * guest 插件（exlaunch subsdk）每帧把本结构写入自分配堆页；宿主 Xnavi
 * 经线性别名 host = arenaLow + guestVA 直读 64 字节。另每 200ms 落一份
 * sdmc:/xnavi/pos.json 作兜底（见 docs/协议-v1.md）。
 *
 * 布局：64 字节，小端。CRC32-IEEE 覆盖字节 [0,44)，存于偏移 44。
 * 宿主 Go 镜像见 xnavi/beacon.go（须与本文件逐字段一致）。
 *
 * 许可证：协议为事实性接口定义，随宿主 MIT；guest 插件 GPL-2.0（见 NOTICE）。
 */
#ifndef XNAVI_BEACON_H
#define XNAVI_BEACON_H

#include <stdint.h>

#define XNAV_BEACON_MAGIC   0x56414E58u  /* "XNAV" 小端 u32 */
#define XNAV_BEACON_SCHEMA  1u
#define XNAV_BEACON_SIZE    64u
#define XNAV_BEACON_CRC_LEN 44u          /* CRC 覆盖 [0,44) */

/* state：声明式游戏状态（消灭宿主侧进庙/站桩启发式猜测） */
enum xnav_state {
    XNAV_STATE_OVERWORLD = 0,  /* 大世界 */
    XNAV_STATE_SHRINE    = 1,  /* 神庙内部（本地坐标） */
    XNAV_STATE_TRIAL     = 2,  /* 试炼（剑之试炼等，本地坐标） */
    XNAV_STATE_MENU      = 3,  /* 菜单/暂停 */
    XNAV_STATE_LOADING   = 4,  /* 加载/过图 */
    XNAV_STATE_DEAD      = 5,  /* 死亡 */
    XNAV_STATE_MEMORY    = 6   /* 回忆影片 */
};

/* flags 位定义 */
#define XNAV_FLAG_GROUNDED  (1u << 0)

#pragma pack(push, 1)
typedef struct {
    uint32_t magic;         /* 0  XNAV_BEACON_MAGIC */
    uint16_t schema;        /* 4  XNAV_BEACON_SCHEMA */
    uint16_t build;         /* 6  mod 构建号 */
    uint32_t seq;           /* 8  帧计数，每次写入 +1（宿主判活：冻结=插件停摆） */
    float    x;             /* 12 世界/本地 X */
    float    y;             /* 16 海拔 Y */
    float    z;             /* 20 世界/本地 Z */
    float    yaw;           /* 24 朝向（弧度） */
    uint32_t state;         /* 28 enum xnav_state */
    uint64_t gameClock;     /* 32 游戏内时间编码 */
    uint32_t actorPtrHash;  /* 40 演员指针哈希（诊断） */
    uint32_t crc;           /* 44 CRC32-IEEE of bytes[0,44) */
    float    hp;            /* 48 生命（可选） */
    float    stamina;       /* 52 精力（可选） */
    uint32_t flags;         /* 56 XNAV_FLAG_* */
    uint32_t reserved;      /* 60 保留 */
} xnav_beacon_t;            /* = 64 字节 */
#pragma pack(pop)

/* guest 侧：计算并填充 crc（CRC32-IEEE，多项式 0xEDB88320，初值 0xFFFFFFFF，终值取反）。
 * 与 Go hash/crc32.ChecksumIEEE 一致。 */
uint32_t xnav_beacon_crc(const xnav_beacon_t *b);

#endif /* XNAVI_BEACON_H */