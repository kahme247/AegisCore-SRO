cmake_minimum_required(VERSION 3.20)
project(AegisClientDiagnostics LANGUAGES CXX)

# The injected DLL must not depend on a developer machine's C++ runtime.
set(CMAKE_MSVC_RUNTIME_LIBRARY "MultiThreaded$<$<CONFIG:Debug>:Debug>")

add_library(AegisClientDiagnostics SHARED
    src/dllmain.cpp
    src/client_trace.cpp
)

target_compile_features(AegisClientDiagnostics PRIVATE cxx_std_17)
target_compile_definitions(AegisClientDiagnostics PRIVATE WIN32_LEAN_AND_MEAN NOMINMAX)
target_link_libraries(AegisClientDiagnostics PRIVATE bcrypt)
set_target_properties(AegisClientDiagnostics PROPERTIES OUTPUT_NAME "AegisClientDiagnostics")

add_executable(AegisClientDiagnosticsTest
    src/trace_core_test.cpp
    src/client_trace.cpp
)
target_compile_features(AegisClientDiagnosticsTest PRIVATE cxx_std_17)
target_compile_definitions(AegisClientDiagnosticsTest PRIVATE WIN32_LEAN_AND_MEAN NOMINMAX)
target_link_libraries(AegisClientDiagnosticsTest PRIVATE bcrypt)

add_executable(AegisClientDiagnosticsInjector src/injector.cpp)
target_compile_features(AegisClientDiagnosticsInjector PRIVATE cxx_std_17)
target_compile_definitions(AegisClientDiagnosticsInjector PRIVATE WIN32_LEAN_AND_MEAN NOMINMAX)
