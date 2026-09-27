# Agent Instructions

## Behavioral Guidelines
Behavioral guidelines to reduce common LLM coding mistakes.
**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.
### 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:

- State assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them—don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

### 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

### 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:

- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it—don't delete it.

When your changes create orphans:

- Remove imports, variables, or functions that **your** changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

### 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:

- "Add validation" → "Write tests for invalid inputs, then make them pass."
- "Fix the bug" → "Write a test that reproduces it, then make it pass."
- "Refactor X" → "Ensure tests pass before and after."

For multi-step tasks, state a brief plan:

```text
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

**These guidelines are working if:** fewer unnecessary changes appear in diffs, fewer rewrites happen due to overcomplication, and clarifying questions come before implementation rather than after mistakes.

## Protocol And Reverse Engineering

- Always use the configured Pi IDA MCP server to discover the correct opcode and understand how the application sends and receives the request before implementing or changing protocol behavior.
- Begin IDA work with `ida_status`, select the port for the intended database, and pass that `port` to every subsequent IDA tool call. Load the `ida-reverse-engineering` skill before using the workflow.
- Support conclusions with address-backed evidence: identify the binary, image base, effective address, relevant field offsets, and corroborating callers, xrefs, disassembly, bytes, or pseudocode. Treat IDA/Lumina names as hypotheses until verified.
- Before any IDA database mutation, state the intended change and its evidence. Do not patch binary bytes unless the user explicitly requests it and the IDA configuration explicitly permits patching.
- If IDA cannot establish the behavior, fall back to the sanitized protocol evidence and references in `REFERENCES.md`, `protocol/`, and the applicable capture documentation. Do not invent opcodes, packet layouts, or request/response behavior.
