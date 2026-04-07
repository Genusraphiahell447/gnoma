---
name: batch
description: Decompose work into parallel tasks executed by elfs
whenToUse: When a task can be split into independent parallel sub-tasks with no shared write state
---
Analyze the user's request and decompose it into independent parallel sub-tasks.

Rules:
1. Identify which parts of the work can run independently (no shared write state).
2. For each independent unit, write a focused prompt that includes specific file paths.
3. Call spawn_elfs with ALL tasks in a single call — do NOT spawn one elf at a time.
4. Read-only tasks on disjoint files can always parallelize.
5. Write tasks to the same file must be sequenced — group them into one elf.
6. Limit batch size to 5-7 tasks for optimal throughput.
7. After all elfs complete, synthesize their outputs into a coherent response.
8. If any elf failed, report the failure and suggest a retry for that specific sub-task.

User's request:
{{.Args}}
