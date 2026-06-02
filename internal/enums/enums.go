package enums

var InContext = map[string][]string{
	"auth_storage":      {"auto", "keychain", "file"},
	"collision_policy":  {"skip", "rename", "overwrite"},
	"media_type":        {"photo", "video"},
	"tag_match":         {"all", "any"},
	"sort":              {"recorded_at", "created_at", "updated_at", "name", "filename"},
	"direction":         {"asc", "desc"},
	"download_status":   {"planned", "downloaded", "skipped", "renamed", "overwritten", "failed"},
	"upload_status":     {"planned", "completed", "failed"},
	"share_access":      {"public_link"},
	"credential_source": {"env", "profile"},
}
