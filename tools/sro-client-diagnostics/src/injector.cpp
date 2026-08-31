#include <windows.h>

#include <cstdio>
#include <string>

int wmain(int argc, wchar_t* argv[]) {
    if (argc != 3) {
        std::fwprintf(stderr, L"Usage: AegisClientDiagnosticsInjector.exe <process-id> <absolute-dll-path>\n");
        return 2;
    }

    const DWORD processId = wcstoul(argv[1], nullptr, 10);
    const std::wstring dllPath(argv[2]);
    if (processId == 0 || dllPath.empty() || GetFileAttributesW(dllPath.c_str()) == INVALID_FILE_ATTRIBUTES) {
        std::fwprintf(stderr, L"Invalid process ID or DLL path.\n");
        return 3;
    }

    HANDLE process = OpenProcess(PROCESS_CREATE_THREAD | PROCESS_QUERY_INFORMATION | PROCESS_VM_OPERATION | PROCESS_VM_WRITE | PROCESS_VM_READ, FALSE, processId);
    if (process == nullptr) {
        std::fwprintf(stderr, L"OpenProcess failed: %lu\n", GetLastError());
        return 4;
    }

    const SIZE_T byteCount = (dllPath.length() + 1) * sizeof(wchar_t);
    void* remotePath = VirtualAllocEx(process, nullptr, byteCount, MEM_RESERVE | MEM_COMMIT, PAGE_READWRITE);
    if (remotePath == nullptr) {
        std::fwprintf(stderr, L"VirtualAllocEx failed: %lu\n", GetLastError());
        CloseHandle(process);
        return 5;
    }

    SIZE_T written = 0;
    if (!WriteProcessMemory(process, remotePath, dllPath.c_str(), byteCount, &written) || written != byteCount) {
        std::fwprintf(stderr, L"WriteProcessMemory failed: %lu\n", GetLastError());
        VirtualFreeEx(process, remotePath, 0, MEM_RELEASE);
        CloseHandle(process);
        return 6;
    }

    const auto loadLibrary = reinterpret_cast<LPTHREAD_START_ROUTINE>(GetProcAddress(GetModuleHandleW(L"kernel32.dll"), "LoadLibraryW"));
    HANDLE thread = CreateRemoteThread(process, nullptr, 0, loadLibrary, remotePath, 0, nullptr);
    if (thread == nullptr) {
        std::fwprintf(stderr, L"CreateRemoteThread failed: %lu\n", GetLastError());
        VirtualFreeEx(process, remotePath, 0, MEM_RELEASE);
        CloseHandle(process);
        return 7;
    }

    const DWORD wait = WaitForSingleObject(thread, 10000);
    DWORD moduleHandle = 0;
    const bool loaded = wait == WAIT_OBJECT_0 && GetExitCodeThread(thread, &moduleHandle) && moduleHandle != 0;
    CloseHandle(thread);
    VirtualFreeEx(process, remotePath, 0, MEM_RELEASE);
    CloseHandle(process);

    if (!loaded) {
        std::fwprintf(stderr, L"LoadLibraryW did not complete successfully.\n");
        return 8;
    }

    std::wprintf(L"Aegis client diagnostics armed in process %lu.\n", processId);
    return 0;
}
