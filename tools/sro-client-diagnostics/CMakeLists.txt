cmake_minimum_required(VERSION 3.20)
project(AegisClient LANGUAGES CXX)

# The injected DLL must not depend on a developer machine's C++ runtime.
set(CMAKE_MSVC_RUNTIME_LIBRARY "MultiThreaded$<$<CONFIG:Debug>:Debug>")

add_library(Aegis SHARED
    src/dllmain.cpp
    src/client_trace.cpp
    src/auth/launch_context.cpp
    src/compat/navigation_policy.cpp
    src/core/runtime.cpp
    src/network/gateway_login.cpp
)

target_compile_features(Aegis PRIVATE cxx_std_17)
target_compile_definitions(Aegis PRIVATE WIN32_LEAN_AND_MEAN NOMINMAX)
target_include_directories(Aegis PRIVATE src)
target_link_libraries(Aegis PRIVATE bcrypt)
set_target_properties(Aegis PROPERTIES OUTPUT_NAME "Aegis")

option(AEGIS_ENABLE_DEVKIT_INTEGRATION "Build the guarded reconstructed DevKit integration" OFF)
set(AEGIS_DEVKIT_SOURCE_DIR "" CACHE PATH "Path to DevKit_DLL_reconstructed")
if(AEGIS_ENABLE_DEVKIT_INTEGRATION)
    if(NOT EXISTS "${AEGIS_DEVKIT_SOURCE_DIR}/include/devkit/install_hooks.hpp")
        message(FATAL_ERROR "AEGIS_DEVKIT_SOURCE_DIR must point to DevKit_DLL_reconstructed")
    endif()
    file(GLOB AEGIS_DEVKIT_SOURCES CONFIGURE_DEPENDS "${AEGIS_DEVKIT_SOURCE_DIR}/src/*.cpp")
    list(FILTER AEGIS_DEVKIT_SOURCES EXCLUDE REGEX "[/\\\\]dllmain\\.cpp$")
    target_sources(Aegis PRIVATE src/compat/devkit_runtime.cpp ${AEGIS_DEVKIT_SOURCES})
    target_include_directories(Aegis PRIVATE "${AEGIS_DEVKIT_SOURCE_DIR}/include")
    target_compile_definitions(Aegis PRIVATE AEGIS_WITH_DEVKIT=1)
endif()

add_executable(AegisClientTest
    src/trace_core_test.cpp
    src/client_trace.cpp
)
target_compile_features(AegisClientTest PRIVATE cxx_std_17)
target_compile_definitions(AegisClientTest PRIVATE WIN32_LEAN_AND_MEAN NOMINMAX)
target_link_libraries(AegisClientTest PRIVATE bcrypt)

add_executable(AegisAuthTest
    src/auth/launch_context_test.cpp
    src/auth/launch_context.cpp
)
target_compile_features(AegisAuthTest PRIVATE cxx_std_17)
target_compile_definitions(AegisAuthTest PRIVATE WIN32_LEAN_AND_MEAN NOMINMAX)
target_include_directories(AegisAuthTest PRIVATE src)

add_executable(AegisInjector src/injector.cpp)
target_compile_features(AegisInjector PRIVATE cxx_std_17)
target_compile_definitions(AegisInjector PRIVATE WIN32_LEAN_AND_MEAN NOMINMAX)
