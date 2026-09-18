#pragma once

#include <windows.h>

namespace aegis::compat {

// Queues initialization outside the loader-lock path. The launch-context flag,
// pinned-client SHA-256, and every hook site's original bytes must all match.
bool ScheduleDevKitInitialization(HMODULE aegisModule);

} // namespace aegis::compat
