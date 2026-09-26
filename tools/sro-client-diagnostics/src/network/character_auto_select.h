#pragma once

namespace aegis::network::character_auto_select {

// Schedules one client-side lobby join after the selected character screen exists.
// An empty name deliberately leaves the normal character-selection flow untouched.
void Schedule(const char* characterName);

} // namespace aegis::network::character_auto_select
