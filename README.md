# FootagePal CLI

`footagepal` is a public, agent-friendly CLI for searching FootagePal media metadata and downloading authorized originals.

It talks only to the FootagePal JSON API. It does not scrape Rails HTML, read the database, use Azure account keys, or expose signed storage URLs in normal output.

## Install

```bash
go install github.com/theinventor/footagepal-cli@latest
```

From a checkout:

```bash
go build -o footagepal .
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

Supported filters map to the FootagePal API contract from `theinventor/footagepal#52`:

- `--account-id`
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

## API Caveat

This CLI is built against the FootagePal media API contract from `theinventor/footagepal#52`:

- `GET /api/v1/contents`
- `GET /api/v1/contents/:id`
- `GET /api/v1/contents/:id/download`
- `GET /api/v1/me`
- `GET /api/v1/accounts`

Before a final release, verify that the FootagePal API PR has merged/deployed or that the target API host exposes the same contract.
