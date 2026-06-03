# FootagePal CLI

`footagepal` is a public, agent-friendly CLI for FootagePal media handoffs: search metadata, upload footage, group it into albums, grant account-local album access, create explicit public share URLs, and download authorized originals.

It talks only to the FootagePal JSON API. It does not scrape Rails HTML, read the database, use Azure account keys, or expose signed storage URLs in normal output.

## Install

```bash
go install github.com/theinventor/footagepal-cli/cmd/footagepal@latest
```

From a checkout:

```bash
go build -o footagepal ./cmd/footagepal
./footagepal --help
```

## Authentication

Create or copy an API token in the FootagePal web app, then save it as a profile:

```bash
footagepal auth save \
  --profile main \
  --token fp_your_token_here
```

The command prints only a token fingerprint. New profiles use the OS keychain when available and fall back to a mode-0600 config file. To force file storage for servers or containers:

```bash
footagepal auth save --profile ci --token "$FOOTAGEPAL_API_TOKEN" --storage file
```

Environment variables are also supported and take precedence over the default saved profile:

```bash
export FOOTAGEPAL_API_TOKEN=fp_your_token_here
export FOOTAGEPAL_API_URL=https://footagepal.com
```

Useful auth commands:

```bash
footagepal auth status
footagepal auth list
footagepal auth use main
footagepal auth logout --profile main
footagepal whoami
footagepal accounts list
```

## Search

Search returns JSON by default:

```bash
footagepal media search \
  --account-id 123 \
  --album-id 77 \
  --start 2024-03-01T00:00:00Z \
  --end 2024-03-31T23:59:59Z \
  --has-gps \
  --near 47.6205,-122.3493 \
  --radius-miles 10 \
  --media-type video \
  --query mountain \
  --limit 50
```

Human table output is available when scanning manually:

```bash
footagepal media search --date 2024-04-02 --human
```

Supported filters map to the FootagePal API contracts from `theinventor/footagepal#52` and `theinventor/footagepal#53`:

- `--account-id`
- `--album-id`
- `--start`, `--end`, `--date`
- `--has-gps=true|false`
- `--near lat,lng`, or `--lat` plus `--lng`
- `--radius-miles`, `--radius-km`
- `--filename`, `--folder`
- `--tag` repeated or comma-separated, `--tag-match all|any`
- `--query`
- `--media-type photo|video`
- `--sort recorded_at|created_at|updated_at|name|filename`
- `--direction asc|desc`
- `--page`, `--per-page`, `--limit`

Fetch one record:

```bash
footagepal media get 42
```

## Albums and Access

Albums are account-scoped media handoff workspaces. JSON is the default output; add `--human` when scanning manually.

```bash
footagepal albums list --account-id 123
footagepal albums show 77 --account-id 123
footagepal albums create --account-id 123 --name "June Handoff"
footagepal albums update 77 --account-id 123 --name "June Drone Handoff"
```

Attach or detach media that the token can edit in the selected account:

```bash
footagepal albums contents list 77 --account-id 123 --media-type video
footagepal albums contents add 77 42 --account-id 123
footagepal albums contents remove 77 42 --account-id 123
```

Grant album access to a current-account user. The API resolves users only inside the selected account.

```bash
footagepal albums access list 77 --account-id 123
footagepal albums access add 77 --account-id 123 --email employee-b@example.com
footagepal albums access add 77 --account-id 123 --user-id 9
footagepal albums access remove 77 9 --account-id 123
```

## Upload

Uploads require an explicit account id. The CLI asks FootagePal for a short-lived signed upload URL, streams file bytes to that URL, then calls the completion endpoint so FootagePal creates or reuses the media record.

Plan one or many uploads without contacting the API or fetching signed URLs:

```bash
footagepal media upload ./clip-a.mp4 ./clip-b.mp4 \
  --account-id 123 \
  --album-id 77 \
  --tag handoff \
  --dry-run
```

Upload after reviewing the plan:

```bash
footagepal media upload ./clip-a.mp4 ./clip-b.mp4 \
  --account-id 123 \
  --album-id 77 \
  --tag handoff \
  --metadata source=employee-a \
  --yes
```

Useful upload flags:

- `--account-id` is required.
- `--album-id` attaches completed uploads to an album when authorized.
- `--user-id` attributes uploads to another current-account user when the API permits it.
- `--storage-bucket-id` selects an account-local storage bucket when authorized.
- `--content-type` overrides extension-based content type detection.
- `--tag` can be repeated or comma-separated.
- `--metadata key=value` and `--metadata-json '{"source":"cli"}'` send completion metadata.
- `--retries` retries direct upload and completion attempts; files are reopened for retry.

Upload safety behavior:

- Upload paths must be explicit files. Directories are refused; there is no recursive sync, watch mode, or daemon.
- Multiple files or more than 100 MiB total require `--dry-run` or `--yes`.
- Signed upload URLs are treated as sensitive bearer URLs and are not printed or persisted.
- Dry-run stops before API preflight, so it cannot fetch a signed upload URL.

## Share URLs

Public share links require an explicit action. Anyone with a returned share URL can view or download that content until server-side share-key semantics change.

```bash
footagepal media share-url 42 --account-id 123 --dry-run
footagepal media share-url 42 --account-id 123 --yes
```

## Download

Downloads always require an explicit output directory.

Download exact media ids:

```bash
footagepal media download 42 43 --out ./footagepal-downloads
```

Plan a search-driven download without writing files:

```bash
footagepal media download \
  --out ./footagepal-downloads \
  --album-id 77 \
  --start 2024-03-01 \
  --end 2024-03-31 \
  --has-gps \
  --dry-run
```

Run a search-driven download after reviewing the dry run:

```bash
footagepal media download \
  --out ./footagepal-downloads \
  --start 2024-03-01 \
  --end 2024-03-31 \
  --has-gps \
  --limit 100 \
  --collision rename \
  --yes
```

Download safety behavior:

- Target names default to `<id>_<safe filename>`.
- `--collision=skip|rename|overwrite` defaults to `skip`.
- Existing symlink targets are refused.
- Files are written through temporary `.part` files and atomically renamed.
- Signed download URLs are treated as sensitive bearer URLs and are not printed.
- Search-driven downloads require `--dry-run` or `--yes`.

## Employee Handoff Flow

1. Employee A saves a FootagePal profile:

   ```bash
   footagepal auth save --profile employee-a --token fp_employee_a_token
   ```

2. Employee A creates or finds a handoff album:

   ```bash
   footagepal --profile employee-a albums create --account-id 123 --name "June Handoff"
   ```

3. Employee A uploads footage into the album:

   ```bash
   footagepal --profile employee-a media upload ./clip-a.mp4 \
     --account-id 123 \
     --album-id 77 \
     --tag handoff \
     --yes
   ```

4. An account admin/member grants Employee B album access:

   ```bash
   footagepal albums access add 77 --account-id 123 --email employee-b@example.com
   ```

5. Employee B verifies access and downloads through normal authorized APIs:

   ```bash
   footagepal --profile employee-b media search --account-id 123 --album-id 77 --human
   footagepal --profile employee-b media download --account-id 123 --album-id 77 --out ./handoff --dry-run
   footagepal --profile employee-b media download --account-id 123 --album-id 77 --out ./handoff --yes
   ```

6. If a public link is explicitly needed and the token has share permission:

   ```bash
   footagepal media share-url 42 --account-id 123 --yes
   ```

## Agent Context

Agents can inspect the command surface, flags, enums, profiles, and exit codes:

```bash
footagepal agent-context
```

Stable exit codes:

- `0` success
- `1` generic error
- `2` usage error
- `3` authentication or authorization failure
- `4` not found
- `5` validation failure
- `6` server error
- `7` network or transport failure
- `8` conflict

## Release Notes

The next release adds `albums`, `albums contents`, `albums access`, `media upload`, `media share-url`, and `--album-id` search/download support for employee handoffs.

## API Caveat

This CLI is built against the FootagePal media API contracts from `theinventor/footagepal#52` and `theinventor/footagepal#53`:

- `GET /api/v1/contents`
- `GET /api/v1/contents/:id`
- `GET /api/v1/contents/:id/download`
- `POST /api/v1/contents/:id/share`
- `GET /api/v1/albums`
- `POST /api/v1/albums`
- `GET /api/v1/albums/:id`
- `PATCH /api/v1/albums/:id`
- `GET /api/v1/albums/:album_id/contents`
- `POST /api/v1/albums/:album_id/contents`
- `DELETE /api/v1/albums/:album_id/contents/:content_id`
- `GET /api/v1/albums/:album_id/users`
- `POST /api/v1/albums/:album_id/users`
- `DELETE /api/v1/albums/:album_id/users/:user_id`
- `POST /api/v1/uploads`
- `POST /api/v1/uploads/complete`
- `GET /api/v1/me`
- `GET /api/v1/accounts`

Before a final release, verify that the target API host exposes the same contract.
