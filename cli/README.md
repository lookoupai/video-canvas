# video-canvas CLI

Requires Node.js 22.19 or later. Run commands with node cli/vc.mjs from this repository or install the package in a private development environment and use vc.

The CLI calls the public user API with a user's own account. It stores the bearer token under the operating system's user config directory with owner-only file permissions. Login passwords are read from standard input and never accepted as command-line arguments. Use a trusted HTTPS site. HTTP is allowed for loopback development.

    printf '%s\n' "$VC_PASSWORD" | node cli/vc.mjs auth login --site https://video.example --username my-user
    node cli/vc.mjs project list --json
    node cli/vc.mjs asset upload --file ./reference.mp4 --json
    node cli/vc.mjs reference add --project 123 --request-key reference-1 --input reference.json
    node cli/vc.mjs reference prepare --project 123 --reference 456 --request-key prepare-1 --input prepare.json
    node cli/vc.mjs draft create --project 123 --reference 456 --request-key analysis-1 --input draft.json
    node cli/vc.mjs draft quote --project 123 --reference 456 --draft 789 --input quote.json
    node cli/vc.mjs draft apply --project 123 --reference 456 --draft 789 --request-key apply-1 --input apply.json
    node cli/vc.mjs draft generate --project 123 --reference 456 --draft 789 --request-key generate-1 --input generate.json
    node cli/vc.mjs segment generate --segment 321 --request-key generate-shot-1 --input generate.json
    node cli/vc.mjs task wait --task 654 --timeout 600
    node cli/vc.mjs episode compose --project 123 --episode 1
    node cli/vc.mjs auth logout

Mutation commands that accept request-key save the input fingerprint before sending. A retry must reuse the key and identical JSON. The CLI rejects reuse of a key with changed input. Task wait timeouts print the existing task ID for a later query. Output errors use distinct exit codes for invalid arguments, authentication, transport uncertainty, platform rejection, timeout, and task failure. Add --json for machine-readable results.

Draft, quote, apply and batch generation input follow the JSON fields in the authenticated API and the reference import contract. The CLI does not run media analysis or estimate provider charges locally. Quote is a preview. Applying creates a follow episode and its shot segments. Batch generation requires an existing application receipt, the same quoted plan digest, and the user's current confirmation before it submits billable platform tasks. Episode compose joins selected outputs and reports the durable project job.

Asset uploads use the platform's existing multipart endpoint, which does not accept an idempotency key. If its response is lost, check the asset library before uploading again. All later draft, reference, generation, and job operations use their server request key or idempotency header where the API supports one.

The local request journal contains hashes and task request payloads can include project text. Keep the CLI config directory private and do not commit it.
