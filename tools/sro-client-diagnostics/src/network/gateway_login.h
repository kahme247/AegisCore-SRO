#pragma once

namespace aegis::network {

// Schedules the launcher-authenticated Gateway login on the stock client's UI
// thread. The implementation is pinned to the verified vSRO 1.188 image and
// disables itself if its code/vtable signatures do not match.
void InstallGatewayAutoLogin();

} // namespace aegis::network
