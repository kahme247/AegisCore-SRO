#include "core/runtime.h"

#include "auth/launch_context.h"
#include "compat/debug_render_commands.h"
#include "compat/navigation_policy.h"
#include "compat/temporary_enhancement_tooltip.h"
#ifdef AEGIS_WITH_DEVKIT
#include "compat/devkit_runtime.h"
#endif
#include "diagnostics/crash_integration.h"
#include "network/gateway_login.h"

namespace aegis::core {

bool Initialize(HINSTANCE module) {
    if (module == nullptr) {
        return false;
    }

    DisableThreadLibraryCalls(module);
    auth::InitializeFromLauncher();
    compat::InstallDebugRenderCommands();
    compat::InstallNavigationPolicy();
    compat::InstallTemporaryEnhancementTooltip();
    diagnostics::InstallCrashIntegration();
    network::InstallGatewayAutoLogin();
#ifdef AEGIS_WITH_DEVKIT
    compat::ScheduleDevKitInitialization(module);
#endif
    return true;
}

} // namespace aegis::core
