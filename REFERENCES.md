# AegisCore reference catalog

This catalog records the external research sources used while recovering and
expanding AegisCore for the pinned **vSRO 1.188** client. A reference is evidence,
not proof: version differences, incomplete implementations and guessed fields
are common throughout historical Silkroad projects.

Do not vendor or copy third-party code without first confirming its license and
provenance. Do not run historical binaries, loaders or archive downloads on a
development machine. Prefer static source inspection inside an ignored local
research directory.

## Main references and evidence order

The two main external documentation sources for AegisCore are:

1. [DummkopfOfHachtenduden/SilkroadDoc](https://github.com/DummkopfOfHachtenduden/SilkroadDoc/wiki)
   for version-pinned vSRO 1.188 packet and file-format evidence;
2. [SRODevs documentation](https://srodevs.gitbook.io/docs) for the primary
   browsable packet catalog and broader development cross-reference.

Use both during protocol research. SilkroadDoc has the stronger version match;
SRODevs often has the more accessible catalog, but each page still needs its
client profile verified. Captured bytes remain the final authority.

When sources disagree, use this order:

1. sanitized wire bytes captured from the exact pinned stock vSRO 1.188 client;
2. the main documentation references: version-pinned SilkroadDoc first, then
   SRODevs when the page's packet/profile scope is verified;
3. Phoenix behavior because it explicitly targets the Vietnam v188 client;
4. the vSRO 188 profile/readers in RSBot;
5. agreement between multiple independent file readers or emulators;
6. original Ferdoran GO-SRO behavior;
7. one historical implementation, recorded as inference and protected by a
   focused test.

Never resolve a conflict by silently mixing structures from different client
versions.

## Main documentation references

### DummkopfOfHachtenduden/SilkroadDoc

- Repository: <https://github.com/DummkopfOfHachtenduden/SilkroadDoc>
- Wiki: <https://github.com/DummkopfOfHachtenduden/SilkroadDoc/wiki>
- Packet index: <https://github.com/DummkopfOfHachtenduden/SilkroadDoc/wiki/Packets>
- Target: vSRO 1.188.
- Status: archived and read-only since June 2025.
- Use for: security handshake, framework packets, Gateway/Agent opcodes,
  `0x600D` massive messages, character/entity state, movement, inventory,
  exchange, party, skills, JMX and PK2 file formats.
- Rule: published documentation is strong evidence but remains below captured
  bytes. The wiki notes that not every packet layout was released.
- Wiki snapshot inspected on 2026-07-16: commit
  `21e8ccdeaecb7ad7d641352ffc78da287377ec9c`. No clear content/code license
  was found, so use independently verified protocol facts rather than copied
  prose or implementation.

### SRODevs documentation

- Source: <https://srodevs.gitbook.io/docs>
- Complete index: <https://srodevs.gitbook.io/docs/llms.txt>
- Markdown home: <https://srodevs.gitbook.io/docs/readme.md>
- Packet index: <https://srodevs.gitbook.io/docs/packets>
- Agent packet index: <https://srodevs.gitbook.io/docs/packets/agent>
- Scope: a community compilation covering file formats, packets, Lua, CScript
  and client internals; the top-level documentation does not pin all pages to a
  single Silkroad client version.
- Use for: a maintained, readable packet catalog, opcode direction, packet
  names, message-type conventions, and cross-checking Gateway and Agent packet
  research.
- Rule: treat it as strong public documentation, but verify the packet's client
  profile before applying a layout to vSRO 1.188. Captures and version-pinned
  SilkroadDoc evidence take precedence when the sources differ.
- Website inspected on 2026-07-18. The packet and Agent indexes reported a
  recent update, but GitBook exposes no repository revision in the public page;
  record the page URL and inspection date when adopting a fact.

## Supporting version-specific references

### Phoenix

- Source: <https://gitlab.com/Chernobyl_/phoenix>
- Target: Vietnam v188.
- Use for: key exchange, framed sessions, Gateway/Agent boundaries, opcode
  direction, character admission, entity state and DB/runtime organization.
- Caution: incomplete and inactive since 2019; confirm licensing before copying
  code. Compare behavior and tests rather than architecture age.
- Research snapshot inspected on 2026-07-16: commit
  `95d3e4bde1e28b88ea22bcc2802615577bdd64d9`. The public branch is primarily
  transport/framework code, and no clear license was found in the tree.

### RSBot

- Source: <https://github.com/myildirimofficial/RSBot>
- Historical URL: `https://github.com/SDClowen/RSBot` redirects to the source
  above.
- Targets: includes a named vSRO 188 profile as well as many other SRO clients.
- Use for: exact client-side readers, opcode/profile tables, entity/group spawn,
  `X,Z,Y` wire interpretation, character data, items, NPCs, pets/COS, skills,
  combat, party and interaction flows.
- Caution: always select the vSRO 188 profile; layouts from v193, v274, iSRO,
  BlackRogue and other clients are not interchangeable.
- Research snapshot inspected on 2026-07-16: commit
  `1723fed61b7c75cdb7db58cf04cd5a19dc560fc5`.
- License: AGPL-3.0. Use it to cross-check behavior and fixtures; do not copy
  implementation into AegisCore.

### JellyBitz/SilkroadLauncher

- Source: <https://github.com/JellyBitz/SilkroadLauncher>
- Target: vSRO 1.188.
- Use for: launcher/client profile behavior, host/port configuration, language
  selection, version/update behavior and client-resource handling.

## File, world and reference-data sources

### ferdoran/go-sro-fileutils

- Source: <https://github.com/ferdoran/go-sro-fileutils>
- Relationship: original GO-SRO PK2/navmesh implementation lineage.
- Use for: PK2 reader history, `mapinfo.mfo`, `nv_XXYY.nvm`, `.dat`, `.ifo`,
  1920-unit region geometry and item-data parsing.
- Caution: archived since 2021. AegisCore's bounded safe reader and manifest checks
  take precedence over legacy unchecked behavior.

### JellyBitz/xSROMap

- Source: <https://github.com/JellyBitz/xSROMap>
- Use for: global/internal coordinate conversion, `X,Y,Z,Region` and `x,z,y`
  representation, NPC positions, teleport links and map audit tooling.
- Media inputs documented by the project include `characterdata_*.txt`,
  `textdata_object_all.txt`, `textzonename_all.txt`, `npcpos.txt`,
  `teleportdata.txt`, `teleportbuilding.txt` and `teleportlink.txt`.
- Direct AegisCore value: cross-check spawn axes, NPC placement and teleport
  repository imports.

### JellyBitz/JMX-File-Editor

- Source: <https://github.com/JellyBitz/JMX-File-Editor>
- Use for: independent readers for JMXVRES `.bsr`, JMXVBMT `.bmt`, JMXVCPD
  `.cpd`, `.dof`, `.efp` and especially JMXVENVI `.ifo` files.
- Caution: verify each structure against the pinned Data.pk2 and SilkroadDoc.

### JellyBitz/SRO.PK2API

- Source: <https://github.com/JellyBitz/SRO.PK2API>
- Use for: independent PK2 directory, read, create and mutation behavior.
- Caution: it is a cross-check, not a replacement for AegisCore's fail-closed
  reader; the project documents lowercase-name behavior for new entries.

### JellyBitz/SR_Db2Media

- Source: <https://github.com/JellyBitz/SR_Db2Media>
- Use for: understanding DB-to-Media extraction, skill-data encryption and
  Media.pk2 import workflows.
- Caution: AegisCore should first build deterministic, read-only import/verification
  paths. Never mutate the canonical client archive during server startup.

### Veykril/GFXFileManager

- Source: <https://github.com/Veykril/GFXFileManager>
- Use for: comparing behavior with the 32-bit Joymax `GFXFileManager.dll`.
- Caution: Windows x86 only; the author labels it work-in-progress and warns
  that wrapped functions may be incorrect or swapped.

### skrillax-dev/silkroad-patch-archiver

- Source: <https://github.com/skrillax-dev/silkroad-patch-archiver>
- Use for: future patch provenance, patch-list sequencing and reproducible
  client-version archives.
- Caution: targets current official patching rather than the pinned v188 client
  and discards some patch metadata.

## Transport and security cross-checks

### halimsamy/Silkroad.Net

- Source: <https://github.com/halimsamy/Silkroad.Net>
- Use for: independent secured-session, handshake, message and async stream
  implementation.
- Caution: not an exact v188 packet authority. Use for transport invariants and
  compare its behavior with captures and Phoenix.
- Research snapshot inspected on 2026-07-16: commit
  `6378edd752017c29b1bcda698c5df5cb56bc83cc` (MIT).

### kumpelblase2/skrillax-network

- Source: <https://github.com/kumpelblase2/skrillax-network>
- Use for: layered Silkroad serialization, packet definitions, TCP framing,
  secured stream establishment and handshake organization.
- Caution: its protocol-independent transport design is more useful to AegisCore
  than any version-specific packet assumption.
- Research snapshot inspected on 2026-07-16: commit
  `20b525964959996957f3d4d13dbbdfe0d539a9c2` (MIT).

### kumpelblase2/skrillax

- Source: <https://github.com/kumpelblase2/skrillax>
- Target: mostly newer iSRO, around v1.584-v1.669 in its documentation.
- Use for: typed packet codecs, ECS/world ownership, navmesh loading, Gateway and
  Agent service organization.
- Caution: do not use its packet layouts for vSRO 1.188 without captures.

## Capture and analysis tools

### skrillax-dev/pickpocket

- Source: <https://github.com/skrillax-dev/pickpocket>
- Historical owner URL: `https://github.com/kumpelblase2/pickpocket` redirects
  to the source above.
- Use for: observing packets at the client read/write boundary and producing
  human-readable traces for later sanitization.
- Caution: injector offsets and hooks are client-version specific. Use only in
  an isolated research environment with an authorized client copy.

### phAnalyzer

- Source: <https://gitlab.com/ryan-projecthax/phAnalyzer>
- Use for: Silkroad packet capture and analyzer workflow.
- Caution: captures must be sanitized according to
  `protocol/captures/README.md` before any fixture is committed.

### ProjectHax/pySilkroadSecurity

- Source: <https://github.com/ProjectHax/pySilkroadSecurity>
- Use for: an MIT-licensed wrapper/proxy interoperability harness around the
  historical SilkroadSecurity API and simple stream-building examples.
- Caution: it wraps native security behavior rather than providing a pure
  Python protocol authority; validate output against AegisCore and pinned-client
  captures.

### XSniff

- Discussion:
  <https://forum.ragezone.com/threads/xsniff-silkroad-online-packet-sniffer.1135553/>
- Use for: historical packet filtering, inspection and vSRO v188 Agent
  redirection workflow descriptions.
- Caution: do not download or execute forum binaries on the development
  machine. Treat claims as leads until reproduced with an authorized client.

### Thessium/open-documentation

- Documentation PDF:
  <https://app.readthedocs.org/projects/open-documentation/downloads/pdf/latest/>
- Use for: vSRO 188 client reverse-engineering vocabulary, structures and
  executable offsets that can guide static analysis.
- Caution: offsets are build-specific and are invalid unless the exact client
  executable hash matches the documented build.

### Silkroad Wireshark dissector

- Source: <https://gist.github.com/kumpelblase2/92857a16220baa4a65b2294f6b1028a1>
- Use for: basic six-byte header inspection, encrypted-bit recognition, TCP
  desegmentation and rapid capture triage.
- Caution: intentionally basic; it is not a complete security or packet decoder.

### buracc/SimpleCL

- Source: <https://github.com/buracc/SimpleCL>
- Use for: request/response ordering across login, character selection,
  inventory, buffs, chat, combat, NPC/shop/stall, pets/COS and teleport flows.
- Caution: clientless/bot behavior and bundled SilkroadSecurityApi assumptions
  are supporting evidence only. The repository URL returned not found during
  the 2026-07-16 refresh, so retain it only as a historical lead unless a
  canonical licensed source is recovered.

## Online opcode-list exclusion rule

Unversioned forum opcode lists are discovery leads only. Do not import them
into `framework/network/opcode` without an exact client profile and a stronger
confirming source. A 2026 generic vSRO list, for example, labels `0x7025` as
movement even though the vSRO 1.188 SilkroadDoc table, RSBot, and AegisCore all use
it for Agent chat. Publication date and apparent list size are not confidence.

The full 2026 online survey, source revisions, conflicts, coverage counts and
recommended registry design are recorded in
[`_docs/vsro188-opcode-online-research.md`](_docs/vsro188-opcode-online-research.md).

## Historical server emulators

Historical emulators are useful because they expose complete gameplay state
machines that may be absent from packet documentation. Their packet layouts are
not automatically compatible with vSRO 1.188.

### Sinien/ClowenEmulationOpenSourceProject

- Source: <https://github.com/Sinien/ClowenEmulationOpenSourceProject>
- Target: SRO v1.315-v1.323.
- Structure: `CLGatewayServer`, `CLGameServer`, `CLFramework`, Entity Framework.
- Use for: service boundaries, handler ownership, entity state and persistence.
- Caution: no clear license was visible during cataloging; research only until
  provenance and license are confirmed.

### SREmu / Route 70

- SourceForge: <https://sourceforge.net/projects/sremu/>
- Target: older international SRO.
- Stack: C++, Lua and MySQL; published as AGPL.
- Use for: early Silkroad framing, scriptable gameplay, DB organization and PK2
  research history.
- Caution: old iSRO layouts and assumptions must not enter v188 serializers.

### Lyzerk C# emulator

- Historical discussion:
  <https://www.elitepvpers.com/forum/sro-pserver-guides-releases/218385-tutorial-silkroad-online-c-emulator-project-lyzerk.html>
- Target: reportedly iSRO v1.176.
- Reported coverage: character deletion, movement, chat, mastery/skills, buffs,
  normal attacks, monsters, potions and inventory/equipment.
- Use for: conceptual mastery, combat, mob and item state sequencing.
- Caution: no verified canonical licensed source repository was found. Treat the
  discussion as a lead and do not download or execute archived binaries.

### SSCv3

- Historical source discussion:
  <https://www.elitepvpers.com/forum/sro-coding-corner/2200877-release-c-sscv3-emulator-source-code.html>
- Target: reportedly `sro_open`, 60 cap.
- Reported coverage: login, character lifecycle, initial spawn, multiple
  movement modes, NPC shops, buffs, mastery/skills, chat and teleport.
- Use for: state-machine comparison only.
- Caution: provenance/license and surviving downloads are unverified.

### SeaShadow / SeaEmu

- Source-release discussion:
  <https://www.elitepvpers.com/forum/sro-coding-corner/1329871-vb6-seashadow-thesource.html>
- Behavior guide:
  <https://www.elitepvpers.com/forum/sro-pserver-guides-releases/636825-guide-seashadow-emulator-everything-you-need-know-about.html>
- Target: older iSRO; VB6 lineage.
- Reported coverage: accounts/characters, drops, NPC trade, stats, ride pets,
  monster variants, teleport, PvP, item mall, skills and auto-attacks.
- Caution: community posts describe recovered or leaked source. Do not copy the
  code or acquire unverified packages; use public behavior descriptions only.

### JaveQ

- Historical discussion:
  <https://rune-server.org/threads/javeq-open-source-silkroad-emulator.180879/>
- Target: older iSRO; Java.
- Use for: historical packet encoder/decoder and LoginServer/GameServer package
  organization.
- Caution: original SVN/Trac links may be dead and version/provenance are weak.

### Srevolution

- Historical guide:
  <https://silkroadlobby.com/konu/guide-srevolution-emulator-everything-you-need-to-know-about-it.30327/>
- Use for: identifying additional gameplay flows and historical terminology.
- Caution: a canonical, clearly licensed source tree was not found. Do not use
  binary packages as implementation evidence.

### Evangelion v188/BlackRogue leads

- Historical archive discussion:
  <https://silkroadlobby.com/konu/lots-of-old-stuff-from-2012-2013-found-at-my-mediafire-account-squirrel-power.29791/>
- Reported targets: Vietnam v188 and Thailand/BlackRogue variants.
- Potential use: locating names and behaviors worth recapturing with our client.
- Caution: unverified archive provenance and executable safety. Lead only; do
  not download, execute, vendor or cite layouts from the packages.

## Using a historical reference in AegisCore

Before implementing a behavior found in one of these sources:

1. record the repository URL, commit, license and stated client version;
2. identify whether the observation is a decoder, encoder, behavior sequence or
   static data assumption;
3. compare it with SilkroadDoc and at least one independent reader/emulator;
4. capture the equivalent behavior from the pinned v188 client when possible;
5. add a minimal sanitized fixture or focused unit test;
6. label remaining unknown bytes and never guess a variable-length branch;
7. document the adopted evidence beside the test or protocol type.

The best immediate emulator comparisons for the current AegisCore gaps are:

| AegisCore area | First references to compare |
| --- | --- |
| Handshake and admission | Phoenix, SilkroadDoc, Silkroad.Net, skrillax-network |
| Character join and group spawn | Phoenix, RSBot, SilkroadDoc, SSCv3 behavior |
| Coordinate/NPC wall issues | RSBot, xSROMap, go-sro-fileutils, JMX File Editor |
| Mastery and skills | SilkroadDoc, RSBot, Lyzerk behavior, SSCv3 behavior |
| Combat and mob AI | RSBot, SimpleCL, Lyzerk behavior, SeaShadow behavior |
| Pets/COS | RSBot, SimpleCL, SilkroadDoc, SeaShadow behavior |
| Inventory, shops and alchemy | SilkroadDoc, RSBot, SimpleCL, SSCv3 behavior |
| Teleport and world data | xSROMap, Phoenix, SilkroadDoc, JMX File Editor |
| PK2/navmesh | pinned Data.pk2, go-sro-fileutils, SilkroadDoc, SRO.PK2API |
