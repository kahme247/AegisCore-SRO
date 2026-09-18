#include "compat/devkit_runtime.h"

#include "devkit/install_hooks.hpp"
#include "devkit/lifecycle_hooks.hpp"

using NTSTATUS = LONG;
#include <bcrypt.h>

#include <array>
#include <cstdio>
#include <cstring>
#include <cwchar>

namespace aegis::compat {
namespace {

constexpr std::array<unsigned char, 32> kPinnedClientSha256{
    0x28,0x13,0x45,0x24,0x56,0x94,0xAE,0xCF,0x1B,0x73,0xF8,0x45,0xFD,0x41,0x0F,0x3F,
    0x0A,0x04,0x1E,0x29,0xCF,0xEE,0x64,0x57,0x32,0x3C,0x4E,0x67,0xAD,0x6B,0x9D,0x48};

HMODULE g_aegisModule{};

void Log(const char* message) {
    OutputDebugStringA(message);
    wchar_t path[MAX_PATH]{};
    if (g_aegisModule == nullptr || GetModuleFileNameW(g_aegisModule, path, MAX_PATH) == 0) return;
    auto* slash = wcsrchr(path, L'\\');
    if (slash == nullptr) return;
    wcscpy_s(slash + 1, static_cast<std::size_t>(path + MAX_PATH - slash - 1), L"AegisDevKit.log");
    HANDLE file = CreateFileW(path, FILE_APPEND_DATA, FILE_SHARE_READ, nullptr, OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (file == INVALID_HANDLE_VALUE) return;
    DWORD written{}; WriteFile(file, message, static_cast<DWORD>(strlen(message)), &written, nullptr); CloseHandle(file);
}

bool HashRunningClient(std::array<unsigned char,32>& digest) {
    wchar_t path[MAX_PATH]{}; if(GetModuleFileNameW(nullptr,path,MAX_PATH)==0) return false;
    HANDLE file=CreateFileW(path,GENERIC_READ,FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE,nullptr,OPEN_EXISTING,FILE_ATTRIBUTE_NORMAL,nullptr);
    if(file==INVALID_HANDLE_VALUE) return false;
    BCRYPT_ALG_HANDLE algorithm{}; BCRYPT_HASH_HANDLE hash{}; DWORD objectSize{},result{}; bool ok=false;
    if(BCryptOpenAlgorithmProvider(&algorithm,BCRYPT_SHA256_ALGORITHM,nullptr,0)==0 &&
       BCryptGetProperty(algorithm,BCRYPT_OBJECT_LENGTH,reinterpret_cast<PUCHAR>(&objectSize),sizeof(objectSize),&result,0)==0){
        auto* object=static_cast<PUCHAR>(HeapAlloc(GetProcessHeap(),0,objectSize));
        if(object && BCryptCreateHash(algorithm,&hash,object,objectSize,nullptr,0,0)==0){
            std::array<unsigned char,65536> buffer{}; DWORD read{}; ok=true;
            while(ReadFile(file,buffer.data(),static_cast<DWORD>(buffer.size()),&read,nullptr) && read!=0)
                if(BCryptHashData(hash,buffer.data(),read,0)!=0){ok=false;break;}
            if(ok) ok=BCryptFinishHash(hash,digest.data(),static_cast<ULONG>(digest.size()),0)==0;
            BCryptDestroyHash(hash);
        }
        if(object) HeapFree(GetProcessHeap(),0,object);
    }
    if(algorithm) BCryptCloseAlgorithmProvider(algorithm,0); CloseHandle(file); return ok;
}

void ObserveHook(void*, std::uintptr_t address, bool installed) {
    char line[96]{}; sprintf_s(line,"Aegis DevKit: hook 0x%08lX %s\r\n",static_cast<unsigned long>(address),installed?"installed":"failed"); Log(line);
}

void InitializeRegistered(void*) { devkit::InitializeRegisteredFeatures(); }

DWORD WINAPI DevKitWorker(void*) {
    std::array<unsigned char,32> digest{};
    if(!HashRunningClient(digest) || digest!=kPinnedClientSha256){Log("Aegis DevKit: client fingerprint mismatch; disabled\r\n");return 0;}
    if(!devkit::VerifyHookTargets()){Log("Aegis DevKit: hook preflight mismatch; disabled\r\n");return 0;}
    constexpr std::size_t kDiagnosticHookPrefix=8;
    if(!devkit::InstallHookPrefixTransactional(kDiagnosticHookPrefix,nullptr,&ObserveHook)){Log("Aegis DevKit: staged transaction failed and was rolled back\r\n");return 0;}
    Log("Aegis DevKit: staged pure-forwarding interface hook active\r\n");
    // Class registration is kept separate until the CIFMacroGuide factory's
    // world-entry construction path is fully reconstructed.
    Log("Aegis DevKit: guarded integration enabled\r\n"); return 0;
}

} // namespace

bool ScheduleDevKitInitialization(HMODULE aegisModule) {
    g_aegisModule=aegisModule;
    if(!QueueUserWorkItem(&DevKitWorker,nullptr,WT_EXECUTEDEFAULT)){Log("Aegis DevKit: failed to queue initialization\r\n");return false;}
    Log("Aegis DevKit: initialization queued\r\n"); return true;
}

} // namespace aegis::compat
