// main.cpp — Xnavi 信标插件（Eden / BOTW 1.6.0）。
//
// 读取玩家世界坐标，周期性写入一块静态内存信标，供 Xnavi 宿主直接读取，
// 免去宿主侧的内存扫描。信标布局见 beacon.h。
//
// 坐标取玩家 MainBody 变换矩阵的平移分量；插件只读游戏内存、只写自己的
// 信标缓冲，不调用文件系统接口（早期版本尝试写 sdmc 文件会导致游戏卡死）。
//
// 基于 exlaunch 框架，GPL-2.0（见 NOTICE）。

#include "lib.hpp"
#include "nn/os.hpp"
#include "beacon.h"
#include <span>
#include <cstring>

namespace {

// 信标缓冲（.bss，宿主魔数扫描发现）。
xnav_beacon_t g_Beacon __attribute__((aligned(16)));

uint32_t DetectState(float x, float y, float z) {
    if (y < 50.0f && x > -300.0f && x < 300.0f && z > -300.0f && z < 300.0f)
        return XNAV_STATE_SHRINE;
    return XNAV_STATE_OVERWORLD;
}

void UpdateBeacon() {
    // MainBody 矩阵 [[main+2CA1140]+60]+398（12 float，行主序 3x4）。
    // 平移分量 m[3]/m[7]/m[11]（+3A4/+3B4/+3C4）= 世界坐标。
    float* m = exl::util::pointer_path::FollowSafe<float,
        0x2CA1140, 0x60, 0x398>();
    if (!m) return;
    float x = m[3], y = m[7], z = m[11];

    g_Beacon.magic = XNAV_BEACON_MAGIC;
    g_Beacon.schema = XNAV_BEACON_SCHEMA;
    g_Beacon.build = 1;
    g_Beacon.seq++;
    g_Beacon.x = x; g_Beacon.y = y; g_Beacon.z = z;
    g_Beacon.yaw = 0.0f;
    g_Beacon.state = DetectState(x, y, z);
    g_Beacon.gameClock = 0;
    g_Beacon.actorPtrHash = (uint32_t)(uintptr_t)m;
    g_Beacon.hp = 0.0f; g_Beacon.stamina = 0.0f;
    g_Beacon.flags = 0; g_Beacon.reserved = 0;
    g_Beacon.crc = exl::util::Crc32::Hash(
        std::span<const std::byte, XNAV_BEACON_CRC_LEN>(
            reinterpret_cast<const std::byte*>(&g_Beacon), XNAV_BEACON_CRC_LEN));
}

void BeaconThread(void*) {
    svcSleepThread(5000000000LL); // 5s 启动延迟
    for (;;) {
        svcSleepThread(200000000LL); // 200ms
        UpdateBeacon();
    }
}

} // namespace

extern "C" void exl_main(void* x0, void* x1) {
    exl::hook::Initialize();
    std::memset(&g_Beacon, 0, sizeof(g_Beacon));
    Logging.Log("XNAVI_BEACON started main=%p beacon=%p",
        (void*)exl::util::modules::GetTargetStart(), (void*)&g_Beacon);

    static nn::os::ThreadType s_Thread __attribute__((aligned(0x10)));
    static uint8_t s_Stack[0x20000] __attribute__((aligned(0x1000)));
    if (nn::os::CreateThread(&s_Thread, BeaconThread, nullptr,
            s_Stack, sizeof(s_Stack), 16) == 0) {
        nn::os::StartThread(&s_Thread);
    }
}

extern "C" NORETURN void exl_exception_entry() {
    EXL_ABORT("xnavi-beacon exception handler called!");
}