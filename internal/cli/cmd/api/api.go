package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

type Dependencies struct {
	JSONEnabled   func() bool
	DryRunEnabled func() bool
	// LoadConfig resolves configuration, steered by --host. The override is a
	// parameter rather than an environment variable so targeting one instance
	// cannot outlive the command that asked for it.
	LoadConfig func(config.Overrides) (config.AppConfig, error)
	WriteJSON  func(w io.Writer, value any) error
}

func (deps *Dependencies) withDefaults() Dependencies {
	d := *deps
	if d.JSONEnabled == nil {
		d.JSONEnabled = func() bool { return false }
	}
	if d.DryRunEnabled == nil {
		d.DryRunEnabled = func() bool { return false }
	}
	if d.LoadConfig == nil {
		d.LoadConfig = config.LoadWithOverrides
	}
	if d.WriteJSON == nil {
		d.WriteJSON = jsonoutput.Write
	}
	return d
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	var method string
	var host string
	var rawFields []string
	var typedFields []string
	var headers []string
	var inputFile string
	var paginate bool

	cmd := &cobra.Command{
		Use:   "api <path>",
		Short: "Send a raw HTTP request to the Bitbucket REST API",
		Long: `Send a raw HTTP request to the Bitbucket REST API as an escape hatch for uncovered endpoints.

Reuses stored authentication, host aliases, TLS options, retries, and pagination.

Field arguments:
  -f, --raw-field k=v    Pass a string parameter (query parameter for GET, JSON field for POST/PUT/DELETE)
  -F, --field k=v        Pass a typed parameter (parses booleans, numbers, null, JSON, or @file)
  -H, --header k:v       Pass a custom HTTP header
  --input file           Pass a request body from a file (or '-' for stdin)
  --paginate             Automatically fetch all pages for paginated endpoints
  --host url             Target a specific Bitbucket host URL

Note: On Windows Git Bash (MSYS2), set MSYS_NO_PATHCONV=1 or omit the leading slash (e.g. rest/api/1.0/...) to prevent shell path mangling.`,
		Example: `  # GET a pull request settings resource
  bb api /rest/api/1.0/projects/PROJ/repos/repo/settings/pull-requests

  # Target a specific Bitbucket instance
  bb api /rest/api/1.0/projects --host https://bitbucket.example.com

  # Paginate all admin groups
  bb api /rest/api/1.0/admin/groups --paginate

  # POST with typed JSON body fields
  bb api /rest/api/1.0/projects/PROJ/repos/repo/branches -X POST -F name=feature/test -F startPoint=main

  # POST with JSON payload from file or stdin
  bb api /rest/api/1.0/projects/PROJ/repos/repo/branches --method POST --input body.json
  cat body.json | bb api /rest/api/1.0/projects/PROJ/repos/repo/branches -X POST --input -`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rawArgPath := strings.TrimSpace(args[0])
			if rawArgPath == "" {
				return apperrors.New(apperrors.KindValidation, "path cannot be empty", nil)
			}

			path, wasMangled := sanitizeMangledPath(rawArgPath)
			if wasMangled {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: path %q appears to be mangled by shell path conversion; sanitized to %q (set MSYS_NO_PATHCONV=1 to prevent mangling)\n", rawArgPath, path)
			}

			resolvedMethod := strings.ToUpper(strings.TrimSpace(method))
			if resolvedMethod == "" {
				if len(rawFields) > 0 || len(typedFields) > 0 || inputFile != "" {
					resolvedMethod = http.MethodPost
				} else {
					resolvedMethod = http.MethodGet
				}
			}

			if d.DryRunEnabled() {
				if resolvedMethod != http.MethodGet && resolvedMethod != http.MethodHead {
					return apperrors.New(
						apperrors.KindValidation,
						fmt.Sprintf("--dry-run: refusing mutating %s request to %s", resolvedMethod, path),
						nil,
					)
				}
			}

			cfg, err := loadConfigForHost(d, host)
			if err != nil {
				return err
			}

			customHeaders := make(http.Header)
			for _, h := range headers {
				name, val, ok := strings.Cut(h, ":")
				if !ok {
					return apperrors.New(apperrors.KindValidation, fmt.Sprintf("invalid header format %q (expected Name: Value)", h), nil)
				}
				customHeaders.Add(strings.TrimSpace(name), strings.TrimSpace(val))
			}

			var bodyBytes []byte
			queryValues := make(url.Values)

			if inputFile != "" {
				if inputFile == "-" {
					data, err := io.ReadAll(cmd.InOrStdin())
					if err != nil {
						return apperrors.New(apperrors.KindValidation, "failed to read request body from stdin", err)
					}
					bodyBytes = data
				} else {
					data, err := os.ReadFile(inputFile)
					if err != nil {
						return apperrors.New(apperrors.KindValidation, fmt.Sprintf("failed to read input file %s", inputFile), err)
					}
					bodyBytes = data
				}
			}

			// Process fields
			bodyFields := make(map[string]any)
			for _, rf := range rawFields {
				k, v, err := parseField(rf, false)
				if err != nil {
					return err
				}
				if isBodyMethod(resolvedMethod) && inputFile == "" {
					bodyFields[k] = v
				} else {
					queryValues.Add(k, fmt.Sprint(v))
				}
			}
			for _, tf := range typedFields {
				k, v, err := parseField(tf, true)
				if err != nil {
					return err
				}
				if isBodyMethod(resolvedMethod) && inputFile == "" {
					bodyFields[k] = v
				} else {
					queryValues.Add(k, fmt.Sprint(v))
				}
			}

			if isBodyMethod(resolvedMethod) && inputFile == "" && len(bodyFields) > 0 {
				encoded, err := json.Marshal(bodyFields)
				if err != nil {
					return apperrors.New(apperrors.KindValidation, "failed to encode JSON body from fields", err)
				}
				bodyBytes = encoded
			}

			client := httpclient.NewFromConfig(cfg)

			if paginate && resolvedMethod == http.MethodGet {
				return executePaginated(cmd.Context(), client, path, queryValues, customHeaders, d, cmd.OutOrStdout())
			}

			resp, err := client.DoRequest(cmd.Context(), httpclient.RequestOptions{
				Method:  resolvedMethod,
				Path:    path,
				Query:   queryValues,
				Headers: customHeaders,
				Body:    bodyBytes,
			})
			if err != nil {
				return err
			}

			if err := htmlResponseError(resp, path); err != nil {
				return err
			}

			return writeResponse(cmd.OutOrStdout(), resp.Body, d)
		},
	}

	cmd.Flags().StringVarP(&method, "method", "X", "", "HTTP method (GET, POST, PUT, DELETE, PATCH, HEAD)")
	cmd.Flags().StringVar(&host, "host", "", "Bitbucket host URL")
	cmd.Flags().StringArrayVarP(&rawFields, "raw-field", "f", nil, "Add a string parameter (key=value)")
	cmd.Flags().StringArrayVarP(&typedFields, "field", "F", nil, "Add a typed parameter (key=value, booleans, numbers, null, or @file)")
	cmd.Flags().StringArrayVarP(&headers, "header", "H", nil, "Add a custom HTTP request header (Name: Value)")
	cmd.Flags().StringVar(&inputFile, "input", "", "File to use as request body (or '-' for stdin)")
	cmd.Flags().BoolVar(&paginate, "paginate", false, "Automatically fetch all pages for paginated endpoints")

	return cmd
}

func isBodyMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func parseField(field string, isTyped bool) (string, any, error) {
	k, v, ok := strings.Cut(field, "=")
	if !ok {
		return "", nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("invalid field format %q (expected key=value)", field), nil)
	}
	k = strings.TrimSpace(k)
	if k == "" {
		return "", nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("empty key in field %q", field), nil)
	}

	if !isTyped {
		return k, v, nil
	}

	if strings.HasPrefix(v, "@") {
		filePath := strings.TrimPrefix(v, "@")
		fileBytes, err := os.ReadFile(filePath)
		if err != nil {
			return "", nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("failed to read field file %s", filePath), err)
		}
		var parsed any
		if json.Unmarshal(fileBytes, &parsed) == nil {
			return k, parsed, nil
		}
		return k, string(fileBytes), nil
	}

	trimmed := strings.TrimSpace(v)
	switch trimmed {
	case "true":
		return k, true, nil
	case "false":
		return k, false, nil
	case "null":
		return k, nil, nil
	}

	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return k, n, nil
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil && strings.Contains(trimmed, ".") {
		return k, f, nil
	}
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return k, parsed, nil
		}
	}

	return k, v, nil
}

func executePaginated(
	ctx context.Context,
	client *httpclient.Client,
	path string,
	query url.Values,
	headers http.Header,
	deps Dependencies,
	out io.Writer,
) error {
	var aggregatedValues []any
	var finalEnvelope map[string]any
	isFirstPage := true

	currentQuery := make(url.Values)
	for k, vs := range query {
		for _, v := range vs {
			currentQuery.Add(k, v)
		}
	}

	for {
		resp, err := client.DoRequest(ctx, httpclient.RequestOptions{
			Method:  http.MethodGet,
			Path:    path,
			Query:   currentQuery,
			Headers: headers,
		})
		if err != nil {
			return err
		}

		if err := htmlResponseError(resp, path); err != nil {
			return err
		}

		var pageData map[string]any
		if err := json.Unmarshal(resp.Body, &pageData); err != nil {
			// Not a JSON object page, write body directly
			return writeResponse(out, resp.Body, deps)
		}

		rawValues, hasValues := pageData["values"].([]any)
		if !hasValues {
			// Not a standard paginated response envelope
			return writeResponse(out, resp.Body, deps)
		}

		if isFirstPage {
			finalEnvelope = pageData
			isFirstPage = false
		}

		aggregatedValues = append(aggregatedValues, rawValues...)

		isLastPage, _ := pageData["isLastPage"].(bool)
		nextPageStart, hasNext := pageData["nextPageStart"]

		if isLastPage || !hasNext || nextPageStart == nil {
			break
		}

		currentQuery.Set("start", fmt.Sprint(nextPageStart))
	}

	finalEnvelope["values"] = aggregatedValues
	finalEnvelope["size"] = len(aggregatedValues)
	finalEnvelope["isLastPage"] = true
	delete(finalEnvelope, "nextPageStart")

	mergedJSON, err := json.Marshal(finalEnvelope)
	if err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to encode paginated response", err)
	}

	return writeResponse(out, mergedJSON, deps)
}

// loadConfigForHost resolves the configuration to use, honouring --host.
//
// The host is passed into the load so the full resolution path runs against it
// — administrative allowed-host policy, per-host stored credentials and TLS
// material all key off the resolved URL.
//
// The URL is then pinned to the requested host. Resolution falls back to the
// configured default server for a host it has never seen, profile URL included,
// which is right for `bb pr list` and wrong for a flag whose entire purpose is
// to leave the default behind: without the pin, the command answers
// confidently from a server the caller never named.
func loadConfigForHost(d Dependencies, host string) (config.AppConfig, error) {
	trimmedHost := strings.TrimSpace(host)
	if trimmedHost == "" {
		return d.LoadConfig(config.Overrides{})
	}

	if !strings.Contains(trimmedHost, "://") {
		trimmedHost = "https://" + trimmedHost
	}

	cfg, err := d.LoadConfig(config.Overrides{Host: trimmedHost})
	if err != nil {
		return config.AppConfig{}, err
	}

	cfg.BitbucketURL = trimmedHost

	return cfg, nil
}

// htmlResponseError reports the login-page trap: a REST endpoint that answers
// with HTML has not answered at all. Bitbucket serves its login page with a 200,
// so without this the HTML is printed as if it were the resource.
//
// Only /rest/ paths are judged this way. `bb api` also reaches plugin and
// servlet endpoints that legitimately render HTML, and refusing those would
// trade one silent wrong answer for a loud wrong error.
func htmlResponseError(resp *httpclient.RawResponse, path string) error {
	if resp == nil {
		return nil
	}
	if !strings.HasPrefix(strings.TrimPrefix(path, "/"), "rest/") {
		return nil
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return nil
	}

	return apperrors.New(
		apperrors.KindAuthentication,
		"expected JSON, got text/html — the request may have been unauthenticated or sent to the wrong path",
		nil,
	)
}

func sanitizeMangledPath(p string) (string, bool) {
	if strings.HasPrefix(p, "/rest/") || strings.HasPrefix(p, "rest/") ||
		strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p, false
	}

	normalized := strings.ReplaceAll(p, "\\", "/")

	// If there's an embedded /rest/ in a path that starts with a drive letter or msys prefix
	if idx := strings.Index(normalized, "/rest/"); idx > 0 {
		prefix := normalized[:idx]
		if isMsysOrDrivePrefix(prefix) {
			return normalized[idx:], true
		}
	}

	// Starts with /[A-Za-z]:/ (e.g. /C:/rest/...)
	if len(normalized) >= 4 && normalized[0] == '/' && isAlpha(normalized[1]) && normalized[2] == ':' && normalized[3] == '/' {
		return normalized[3:], true
	}

	// Starts with [A-Za-z]:/ (e.g. C:/rest/...)
	if len(normalized) >= 3 && isAlpha(normalized[0]) && normalized[1] == ':' && normalized[2] == '/' {
		return normalized[2:], true
	}

	// Starts with /[A-Za-z]/rest/ (e.g. /c/rest/...)
	if len(normalized) >= 8 && normalized[0] == '/' && isAlpha(normalized[1]) && strings.HasPrefix(normalized[2:], "/rest/") {
		return normalized[2:], true
	}

	return p, false
}

// isMsysOrDrivePrefix reports whether a path prefix looks like the Windows
// install root MSYS2 prepends, which is always anchored on a drive letter:
// "C:/Program Files/Git", "/C:/Program Files/Git" or the short "/c" form.
//
// Matching on the words in that path instead — "program files", "git", "msys" —
// is tempting and wrong: it also matches real Bitbucket endpoints, and
// /plugins/servlet/git-lfs/rest/objects/batch would be silently truncated to
// /rest/objects/batch. A drive letter cannot appear in a legitimate URL path,
// so it is the only signal worth acting on.
func isMsysOrDrivePrefix(prefix string) bool {
	if len(prefix) >= 2 && isAlpha(prefix[0]) && prefix[1] == ':' {
		return true
	}
	if len(prefix) >= 3 && prefix[0] == '/' && isAlpha(prefix[1]) && prefix[2] == ':' {
		return true
	}
	if len(prefix) == 2 && prefix[0] == '/' && isAlpha(prefix[1]) {
		return true
	}
	return false
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func writeResponse(w io.Writer, body []byte, deps Dependencies) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil
	}

	var parsed any
	if err := json.Unmarshal(trimmed, &parsed); err == nil {
		if deps.JSONEnabled() {
			if deps.WriteJSON != nil {
				return deps.WriteJSON(w, parsed)
			}
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			return enc.Encode(parsed)
		}

		// In human mode, format indented JSON
		var buf bytes.Buffer
		if err := json.Indent(&buf, trimmed, "", "  "); err == nil {
			fmt.Fprintln(w, buf.String())
			return nil
		}
	}

	if deps.JSONEnabled() {
		if deps.WriteJSON != nil {
			return deps.WriteJSON(w, string(trimmed))
		}
	}

	_, err := fmt.Fprintln(w, string(trimmed))
	return err
}
