# FootagePal API Contract

This CLI targets the read-only media API added by `theinventor/footagepal#52`.

## Authentication

Every authenticated request sends:

```http
Authorization: token fp_your_token_here
Accept: application/json
```

## Endpoints

```http
GET /api/v1/me
GET /api/v1/accounts
GET /api/v1/contents
GET /api/v1/contents/:id
GET /api/v1/contents/:id/download
```

`GET /api/v1/contents` supports:

- `account_id`
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
