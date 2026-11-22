---
name: video-canvas-reference-remake
description: Use the video-canvas CLI to share reference videos, evidence, drafts, quotes, segment generation, tasks, and episode composition with the web workspace.
---

# Reference remake with video-canvas

## Setup

Use Node.js 22.19 or later. Run `vc --help` to list commands. Authentication uses the public API for the selected site and the user's own account.

Read the password from a private input channel and pipe it to `vc auth login --site SITE --username USERNAME`. Do not put passwords or bearer tokens in command-line arguments, prompts, generated files, shell history, or chat output. Run `vc auth status --json` to verify the account, and `vc auth logout` when finished.

Use `--json` when another program needs to consume a result. Keep uploaded media paths local to the client. Upload first and refer to media using the returned platform asset ID.

## Analyze a reference

1. Read the project and its reference collection with `vc project list --json`, `vc project get --project ID --json`, and `vc reference list --project ID --json`.
2. Read the selected reference using `vc reference get --project ID --reference ID --revision REVISION --json`. Review the existing document and preserve any facts already entered by a person.
3. For a new source, upload it with `vc asset upload --file PATH --json`, then attach it with `vc reference add --project ID --request-key KEY --input request.json --json`.
4. Start or recover media preparation with `vc reference prepare --project ID --reference ID --request-key KEY --input request.json --json`. Poll `vc reference status --project ID --reference ID --preparation ID --json` until it reaches a terminal state.
5. Page through saved frames with `vc reference frames --project ID --reference ID --preparation ID --limit 20 --json`. Request a specific shot using `--shot-id SHOT_ID`. Read image bytes from the returned asset URL only when they are needed for analysis.
6. Store observations, references to prepared frames, prompts, and coverage in an independent draft with `vc draft create --project ID --reference ID --request-key KEY --input draft.json --json`. Do not overwrite the formal reference document.

Use only observations supported by the supplied frames or video. Label uncertain content as inferred or unknown. Do not claim dialogue, identity, movement, or audio analysis without evidence.

## Quote and generate

1. Fetch the latest draft with `vc draft get --project ID --reference ID --draft ID --json`.
2. Request a server quote with `vc draft quote --project ID --reference ID --draft ID --input quote.json --json`. The request must include the current draft revision, a selected model, and one or more shot units.
3. Stop when the plan has blocking issues or `executable` is false. Explain the affected shot and returned issue. A plan preview does not submit work.
4. Let the user review the model, selected source window, output trim, reference media, and platform credit quote. Provider account balance may be unknown.
5. Use `vc draft apply` only with the required review decision and the current draft revision. Generation requires its separate user confirmation. A valid quote alone is not permission to submit a paid generation.
6. Submit the approved segment with `vc segment generate --segment SEGMENT_ID --request-key KEY --input generate.json --json`. The input must match the approved plan. On a transport timeout, reuse the same request key and identical JSON.
7. Follow the returned task with `vc task wait --task TASK_ID --timeout 600 --json`. On timeout, retain the task ID and query it again. Do not submit another generation to recover a missing response.
8. After selecting desired outputs in the web workspace, submit episode composition with `vc episode compose --project PROJECT_ID --episode EPISODE_NUMBER --json`. Query project jobs to inspect its state.

## Retry and output

For a command with `--request-key`, persist the key together with its input until the result is known. Retry the exact request with the same key. The CLI rejects using that key with changed input.

Asset upload does not accept an idempotency key. If its response is lost, inspect the asset library before uploading again.

Return platform IDs, draft revisions, plan issues, credit estimates, task IDs, and terminal states as structured JSON. Do not claim a generation succeeded until its task is terminal and reports success.
