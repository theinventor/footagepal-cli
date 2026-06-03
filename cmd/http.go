package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/theinventor/footagepal-cli/internal/client"
	"github.com/theinventor/footagepal-cli/internal/exitcode"
)

func decodeAPIResponse(resp *http.Response, out any) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return exitcode.Wrap(exitcode.Network, fmt.Errorf("read response body: %w", err))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiStatusError(resp, body)
	}
	if out == nil {
		return nil
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return exitcode.Wrap(exitcode.Generic, fmt.Errorf("parse JSON response from %s: %w", resp.Request.URL.Path, err))
	}
	return nil
}

func apiStatusError(resp *http.Response, body []byte) error {
	bodyText := strings.TrimSpace(string(body))
	if bodyText == "" {
		bodyText = "(empty response body)"
	}
	bodyText = redactSensitiveText(client.RedactURL(bodyText))
	return exitcode.Wrap(
		exitcode.FromHTTPStatus(resp.StatusCode),
		fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, resp.Request.URL.Path, bodyText),
	)
}
