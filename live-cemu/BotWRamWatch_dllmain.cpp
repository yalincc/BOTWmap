
#include "pch.h"
#include <cstdlib>
#include <cstdint>
#include <algorithm>
#include <iostream>
#include <shellapi.h>
#include <array>
#include <format>
#include <thread>
#include <sstream>
#define DELAY 100//66.666666

#pragma warning( disable : 6387 6031)

ULONG_PTR PAUSED = 0x1046f57c;

ULONG_PTR HEALTH = 0x3f50a9f0;
ULONG_PTR X = 0x1055300c;//0x4306f47c;// 0x11344540;
ULONG_PTR Z = 0x10553014;
float x1, z1, x2, z2, hDist,hSpeed;

float Distance2D(float x1, float z1, float x2, float z2) {
    return sqrt(pow(x2 - x1, 2) + pow(z2 - z1, 2));
}
float _byteswap_float(float input) {
    float output;
    BYTE* inputBytes = reinterpret_cast<BYTE*>(&input);
    BYTE* outputBytes = reinterpret_cast<BYTE*>(&output);

    for (int i = 0; i < 4; i++) {
        outputBytes[i] = inputBytes[3 - i];
    }

    return output;
}
double _byteswap_double(double input) {
    double output;
    BYTE* inputBytes = reinterpret_cast<BYTE*>(&input);
    BYTE* outputBytes = reinterpret_cast<BYTE*>(&output);

    for (int i = 0; i < 8; i++) {
        outputBytes[i] = inputBytes[7 - i];
    }

    return output;
}


BYTE *memoryBaseAddress;
template<class... T>
std::string Concat(T... v) {
    std::stringstream q;
    (q << ... << v);
    return q.str();
}
template<typename T>
BOOL CurlPrint(T v) {
    std::string r = std::format("curl -d \"text={}\" http://localhost:3002 -X POST",Concat(v));
    WinExec((LPCSTR)r.c_str(), SW_HIDE);
     return 0;
}


DWORD WINAPI LoopThread(LPVOID dllPtr) {
    HMODULE dll = static_cast<HMODULE>(dllPtr);
    CurlPrint("Thread enter");
    HMODULE cemu = GetModuleHandleA(NULL);
    auto memory_getBase = reinterpret_cast<void* (*)()>(
        GetProcAddress(cemu, "memory_getBase")
        );
    while (1) {
        memoryBaseAddress = static_cast<BYTE*>(memory_getBase());
        /*if (WaitForSingleObject(event, 0) == WAIT_OBJECT_0) {
            return 0;
        }*/
        if ((GetAsyncKeyState(VK_ESCAPE) & 0x8000) && (GetAsyncKeyState(VK_SHIFT) & 0x8000)) {
                    CurlPrint("Exiting!");
                    FreeLibraryAndExitThread(dll, 0);

        }
        DWORD HEALTHVAL = _byteswap_ulong(*reinterpret_cast<DWORD*>(memoryBaseAddress+HEALTH));
        DWORD is_paused = _byteswap_ulong(*reinterpret_cast<DWORD*>(memoryBaseAddress + PAUSED));
        x1 = _byteswap_float(*reinterpret_cast<float*>(memoryBaseAddress + X));
        z1 = _byteswap_float(*reinterpret_cast<float*>(memoryBaseAddress + Z));
        Sleep(DELAY);
        x2 = _byteswap_float(*reinterpret_cast<float*>(memoryBaseAddress + X));
        z2 = _byteswap_float(*reinterpret_cast<float*>(memoryBaseAddress + Z));
        hDist = Distance2D(x1, z1, x2, z2);
        hSpeed = hDist * 3600 / DELAY;
        std::string r = std::format("curl -d \"mem={}&health={}&x={}&z={}&spd={}&paused={}\" http://localhost:3002 -X POST", 
            static_cast<void*>(memoryBaseAddress),
            HEALTHVAL,
            x2, 
            z2, 
            hSpeed,
            is_paused
        );
        WinExec((LPCSTR)r.c_str(), SW_HIDE);
        //CurlPrint(std::format("Base: {} | Addr: {} ",HEALTHVAL, static_cast<void*>(memoryBaseAddress)));


    ;
    }
    return TRUE;
}


BOOL APIENTRY DllMain( HMODULE dll,
                       DWORD  reason,
                       LPVOID lpReserved
                     )
{
    if (reason == DLL_PROCESS_ATTACH) {
        CurlPrint("====== PROCESS_ATTACH ======");
       
        
        //
        CurlPrint("Thread");
        //
        //HANDLE event = CreateEventA(NULL, FALSE, FALSE, NULL);
        //std::thread thr(LoopThread);
        HANDLE loopThread = CreateThread(NULL, 0, LoopThread, dll, 0, NULL);
        CurlPrint("Thread created");
        CurlPrint(std::format("Thread: {}",loopThread));
        //while (true) {
        //    
        //}
    }
        
    return TRUE;
}

