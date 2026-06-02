# FootagePal API Contract

This CLI targets the token-authenticated FootagePal JSON API added by `theinventor/footagepal#52` and extended by `theinventor/footagepal#53`.

## Authentication

Every authenticated request sends:

```http
Authorization: token fp_your_token_here
Accept: application/json
```

Explicit `account_id` values are scoped by the API token. Unauthorized account references return non-leaking not-found style errors.

## Media

```http
GET /api/v1/contents
GET /api/v1/contents/:id
GET /api/v1/contents/:id/download
POST /api/v1/contents/:id/share
```

`GET /api/v1/contents` supports:

- `account_id`
- `album_id`
- `start`, `end`, `date`
- `has_gps`
- `near`, `lat`, `lng`
- `radius_miles`, `radius_km`
- `filename`, `folder`
- `tags`, `tag_match`
- `q` / `query`
- `media_type`
- `sort`, `direction`
- `page`, `per_page`

`GET /api/v1/contents/:id/download` returns a short-lived signed URL. The CLI fetches that URL without printing or persisting it.

`POST /api/v1/contents/:id/share` creates a public share link when authorized. The CLI requires `--yes` or `--dry-run` because the returned URL has public-link semantics.

## Albums

```http
GET /api/v1/albums
POST /api/v1/albums
GET /api/v1/albums/:id
PATCH /api/v1/albums/:id
```

Create/update requests send `account_id` plus an `album` object:

```json
{
  "account_id": "123",
  "album": {
    "name": "June Handoff",
    "archived": false
  }
}
```

Album list/show responses use `albums` or `album` envelopes with `id`, `account_id`, `name`, `archived`, `content_count`, `user_count`, timestamps, and API links.

## Album Contents

```http
GET /api/v1/albums/:album_id/contents
POST /api/v1/albums/:album_id/contents
DELETE /api/v1/albums/:album_id/contents/:content_id
```

The list endpoint accepts the media search filters, including pagination, text search, tags, media type, GPS, and date filters.

Attach requests send:

```json
{
  "account_id": "123",
  "content_id": "42"
}
```

Attach is idempotent on the server. Album and content lookups stay inside the selected account and token policy scope.

## Album Access

```http
GET /api/v1/albums/:album_id/users
POST /api/v1/albums/:album_id/users
DELETE /api/v1/albums/:album_id/users/:user_id
```

Grant requests send either `user_id` or `email`:

```json
{
  "account_id": "123",
  "email": "employee-b@example.com"
}
```

The API resolves users only through the selected account. Removing access uses the account-local user id from the list/add response.

## Uploads

CLI uploads use API preflight, direct signed-URL upload, then API completion.

```http
POST /api/v1/uploads
POST /api/v1/uploads/complete
```

Preflight request:

```json
{
  "account_id": "123",
  "album_id": "77",
  "user_id": "5",
  "filename": "employee-a-clip.mp4",
  "byte_size": 12345678,
  "content_type": "video/mp4",
  "tags": ["handoff"],
  "metadata": {"source": "footagepal-cli"}
}
```

Preflight response contains a short-lived signed upload URL plus required direct-upload headers. The CLI treats the URL as sensitive and never prints it.

Completion request sends the server-generated `blob_name`, `storage_bucket_id`, filename, byte size, content type, optional album/user attribution, tags, and metadata. Completion is idempotent for an accessible existing blob.

## Employee Handoff Sequence

1. Save a profile with a FootagePal API token.
2. Create or find an album in the selected account.
3. Upload local media with `media upload --account-id ... --album-id ...`.
4. Grant Employee B access with `albums access add`.
5. Employee B verifies with `media search --album-id ...` or `albums contents list`.
6. Employee B downloads through `media download`.
7. Create a public share URL only when explicitly needed and authorized.
