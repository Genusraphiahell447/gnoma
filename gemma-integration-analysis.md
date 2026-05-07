> **Note (2026-05-07):** This document describes the `gemini-cli` (Node.js) implementation.
> The specifics — LiteRT-LM runtime, daemon/PID management, `litert-lm pull`, React/Ink UI —
> are Node.js artifacts and do not apply to gnoma. The **conceptually relevant part** is the
> Complexity Rubric and the `GemmaClassifierStrategy` JSON interface, which informed the Go
> `SLMClassifier` design in Phase 3 of `docs/superpowers/plans/2026-05-07-gnoma-roadmap.md`.
> For the Go implementation, see ADR-013 (`docs/essentials/decisions/002-slm-routing.md`).

# Gemini CLI Local Model Routing (/gemma) Architecture

The `/gemma` integration in the `gemini-cli` uses a local LLM to perform "Model Routing". It automatically decides whether to use a cheaper/faster model (Flash) or a more powerful one (Pro) based on the user's request.

## Core Architecture
*   **Engine:** Uses **LiteRT-LM**, a lightweight runtime that serves Gemma models via a Gemini-compatible HTTP API.
*   **Model:** Specifically uses a quantized **Gemma 3 1B** model (`gemma3-1b-gpu-custom`). It's ~1GB and runs locally with low latency (~100-200ms for classification).
*   **Orchestration:** The CLI manages the LiteRT server as a background daemon, tracking its state via PID files and logs.
*   **Integration:** A `GemmaClassifierStrategy` is injected into the core `ModelRouterService`. It flattens recent chat history, sends it to the local Gemma model with a strict "Complexity Rubric," and uses the JSON response to switch models dynamically.

---

## Integration Todo List

### 1. Infrastructure & Asset Management
- [ ] **Platform Detection:** Logic to map OS/Arch to the correct LiteRT-LM binary download URL.
- [ ] **Safe Installer:** Implementation of binary download + SHA256 checksum verification + permission handling (`chmod +x`, macOS quarantine removal).
- [ ] **Model Manager:** Wrapper for the `litert-lm pull` command to download and verify the 1GB Gemma model.

### 2. Process & Server Management
- [ ] **Background Daemon:** Implementation of `spawn(..., { detached: true })` to keep the LiteRT server running independently of the CLI session.
- [ ] **State Tracking:** A PID-file system to manage server lifecycle (start/stop/status) and prevent port collisions.
- [ ] **Auto-Start Logic:** A manager class (`LiteRtServerManager`) that checks server health on CLI startup and launches it if enabled in settings.

### 3. Routing Logic (The "Brain")
- [ ] **Complexity Rubric:** A specialized system prompt that defines what constitutes a "SIMPLE" vs "COMPLEX" task.
- [ ] **Context Flattener:** Utility to compress the last ~4-20 turns of chat history into a prompt suitable for a small 1B model.
- [ ] **Strategy Implementation:** The `GemmaClassifierStrategy` class to handle the local API call, parse the JSON "reasoning," and return the model decision.

### 4. User Experience (CLI & UI)
- [ ] **Management Commands:** Commands like `gemini gemma {setup|start|stop|status|logs}` for lifecycle and troubleshooting.
- [ ] **Slash Command:** A built-in `/gemma` command that queries the local server health and displays a status panel inside a session.
- [ ] **React/Ink UI:** A status component to show visual indicators (green/red) for the binary, model, and server state.

### 5. Configuration & Safety
- [ ] **Scoped Settings:** Separate "User" settings (binary path) from "Workspace" settings (router enabled/disabled for a specific project).
- [ ] **Failure Resilience:** Logic to gracefully fall back to the default model if the local classifier times out or fails.

---

## Routing Prompts

These are the exact prompts used by the `gemini-cli` to force the small 1B model to output structured JSON with strict reasoning criteria.

### 1. The Complexity Rubric
```markdown
### Complexity Rubric
A task is COMPLEX (Choose \`pro\`) if it meets ONE OR MORE of the following criteria:
1.  **High Operational Complexity (Est. 4+ Steps/Tool Calls):** Requires dependent actions, significant planning, or multiple coordinated changes.
2.  **Strategic Planning & Conceptual Design:** Asking "how" or "why." Requires advice, architecture, or high-level strategy.
3.  **High Ambiguity or Large Scope (Extensive Investigation):** Broadly defined requests requiring extensive investigation.
4.  **Deep Debugging & Root Cause Analysis:** Diagnosing unknown or complex problems from symptoms.
A task is SIMPLE (Choose \`flash\`) if it is highly specific, bounded, and has Low Operational Complexity (Est. 1-3 tool calls). Operational simplicity overrides strategic phrasing.
```

### 2. Output Format Enforcement
```markdown
### Output Format
Respond *only* in JSON format like this:
{
  "reasoning": Your reasoning...
  "model_choice": Either flash or pro
}
And you must follow the following JSON schema:
{
  "type": "object",
  "properties": {
    "reasoning": {
      "type": "string",
      "description": "A brief summary of the user objective, followed by a step-by-step explanation for the model choice, referencing the rubric."
    },
    "model_choice": {
      "type": "string",
      "enum": ["flash", "pro"]
    }
  },
  "required": ["reasoning", "model_choice"]
}
You must ensure that your reasoning is no more than 2 sentences long and directly references the rubric criteria.
When making your decision, the user's request should be weighted much more heavily than the surrounding context when making your determination.
```

### 3. The Main System Prompt
```markdown
### Role
You are the **Lead Orchestrator** for an AI system. You do not talk to users. Your sole responsibility is to analyze the **Chat History** and delegate the **Current Request** to the most appropriate **Model** based on the request's complexity.

### Models
Choose between \`flash\` (SIMPLE) or \`pro\` (COMPLEX).
1.  \`flash\`: A fast, efficient model for simple, well-defined tasks.
2.  \`pro\`: A powerful, advanced model for complex, open-ended, or multi-step tasks.

[... Injects COMPLEXITY_RUBRIC here ...]

[... Injects OUTPUT_FORMAT here ...]

### Examples
**Example 1 (Strategic Planning):**
*User Prompt:* "How should I architect the data pipeline for this new analytics service?"
*Your JSON Output:*
{
  "reasoning": "The user is asking for high-level architectural design and strategy. This falls under 'Strategic Planning & Conceptual Design'.",
  "model_choice": "pro"
}
**Example 2 (Simple Tool Use):**
*User Prompt:* "list the files in the current directory"
*Your JSON Output:*
{
  "reasoning": "This is a direct command requiring a single tool call (ls). It has Low Operational Complexity (1 step).",
  "model_choice": "flash"
}
**Example 3 (High Operational Complexity):**
*User Prompt:* "I need to add a new 'email' field to the User schema in 'src/models/user.ts', migrate the database, and update the registration endpoint."
*Your JSON Output:*
{
  "reasoning": "This request involves multiple coordinated steps across different files and systems. This meets the criteria for High Operational Complexity (4+ steps).",
  "model_choice": "pro"
}
**Example 4 (Simple Read):**
*User Prompt:* "Read the contents of 'package.json'."
*Your JSON Output:*
{
  "reasoning": "This is a direct command requiring a single read. It has Low Operational Complexity (1 step).",
  "model_choice": "flash"
}
**Example 5 (Deep Debugging):**
*User Prompt:* "I'm getting an error 'Cannot read property 'map' of undefined' when I click the save button. Can you fix it?"
*Your JSON Output:*
{
  "reasoning": "The user is reporting an error symptom without a known cause. This requires investigation and falls under 'Deep Debugging'.",
  "model_choice": "pro"
}
**Example 6 (Simple Edit despite Phrasing):**
*User Prompt:* "What is the best way to rename the variable 'data' to 'userData' in 'src/utils.js'?"
*Your JSON Output:*
{
  "reasoning": "Although the user uses strategic language ('best way'), the underlying task is a localized edit. The operational complexity is low (1-2 steps).",
  "model_choice": "flash"
}
```

### 4. The Per-Request Prompt Structure
For every routing decision, the CLI flattens the last ~4 turns of chat history and appends the new user request.

```markdown
You are provided with a **Chat History** and the user's **Current Request** below.

#### Chat History:
[... Flattened text of the last 4 turns, excluding tool calls ...]

#### Current Request:
"[... The actual text of what the user just typed ...]"
```
