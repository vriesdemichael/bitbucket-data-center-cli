package diagnostics

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Level string

const (
	LevelError Level = "error"
	LevelWarn  Level = "warn"
	LevelInfo  Level = "info"
	LevelDebug Level = "debug"
)

type Format string

const (
	FormatText  Format = "text"
	FormatJSONL Format = "jsonl"
)

type Config struct {
	Level  Level
	Format Format
}

type Logger struct {
	level  Level
	format Format
	writer io.Writer
	mu     sync.Mutex
}

var (
	outputWriter   io.Writer = os.Stderr
	outputWriterMu sync.RWMutex
)

func SetOutputWriter(writer io.Writer) {
	if writer == nil {
		writer = io.Discard
	}

	outputWriterMu.Lock()
	defer outputWriterMu.Unlock()
	outputWriter = writer
}

func OutputWriter() io.Writer {
	outputWriterMu.RLock()
	defer outputWriterMu.RUnlock()
	return outputWriter
}

func EnabledWriter(enabled bool, writer io.Writer) io.Writer {
	if enabled {
		return writer
	}

	return io.Discard
}

func ParseLevel(value string) (Level, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	switch Level(trimmed) {
	case LevelError, LevelWarn, LevelInfo, LevelDebug:
		return Level(trimmed), nil
	default:
		return "", fmt.Errorf("invalid log level %q", value)
	}
}

func ParseFormat(value string) (Format, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	switch Format(trimmed) {
	case FormatText, FormatJSONL:
		return Format(trimmed), nil
	default:
		return "", fmt.Errorf("invalid log format %q", value)
	}
}

func NewLogger(config Config, writer io.Writer) *Logger {
	if writer == nil {
		writer = io.Discard
	}

	level := config.Level
	if level == "" {
		level = LevelError
	}

	format := config.Format
	if format == "" {
		format = FormatText
	}

	return &Logger{level: level, format: format, writer: writer}
}

func (logger *Logger) Error(message string, fields map[string]any) {
	logger.log(LevelError, message, fields)
}

func (logger *Logger) Warn(message string, fields map[string]any) {
	logger.log(LevelWarn, message, fields)
}

func (logger *Logger) Info(message string, fields map[string]any) {
	logger.log(LevelInfo, message, fields)
}

func (logger *Logger) Debug(message string, fields map[string]any) {
	logger.log(LevelDebug, message, fields)
}

func (logger *Logger) log(level Level, message string, fields map[string]any) {
	if logger == nil || !logger.Enabled(level) {
		return
	}

	sanitized := RedactFields(fields)
	if sanitized == nil {
		sanitized = map[string]any{}
	}

	logger.mu.Lock()
	defer logger.mu.Unlock()

	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	if logger.format == FormatJSONL {
		event := map[string]any{
			"timestamp": timestamp,
			"level":     level,
			"message":   message,
		}
		for key, value := range sanitized {
			if key == "timestamp" || key == "level" || key == "message" {
				continue
			}
			event[key] = value
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintln(logger.writer, string(encoded))
		return
	}

	keys := make([]string, 0, len(sanitized))
	for key := range sanitized {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	builder := strings.Builder{}
	builder.WriteString(timestamp)
	builder.WriteString(" level=")
	builder.WriteString(string(level))
	builder.WriteString(" msg=")
	builder.WriteString(strconvQuote(message))

	for _, key := range keys {
		builder.WriteString(" ")
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(strconvQuote(fmt.Sprintf("%v", sanitized[key])))
	}

	_, _ = fmt.Fprintln(logger.writer, builder.String())
}

func (logger *Logger) Enabled(level Level) bool {
	if logger == nil {
		return false
	}

	return levelRank(level) <= levelRank(logger.level)
}

func levelRank(level Level) int {
	switch level {
	case LevelError:
		return 0
	case LevelWarn:
		return 1
	case LevelInfo:
		return 2
	case LevelDebug:
		return 3
	default:
		return 0
	}
}

func RedactFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}

	sanitized := make(map[string]any, len(fields))
	for key, value := range fields {
		sanitized[key] = redactValue(key, value)
	}

	return sanitized
}

func redactValue(key string, value any) any {
	if isSensitiveKey(key) {
		return "[REDACTED]"
	}

	switch typed := value.(type) {
	case string:
		return redactURLString(typed)
	case map[string]any:
		return RedactFields(typed)
	case map[string]string:
		converted := make(map[string]any, len(typed))
		for nestedKey, nestedValue := range typed {
			converted[nestedKey] = nestedValue
		}
		return RedactFields(converted)
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, redactValue("", item))
		}
		return items
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if normalized == "" {
		return false
	}

	sensitiveTokens := []string{"token", "password", "secret", "authorization", "cookie", "set-cookie", "apikey", "api-key", "credential"}
	for _, token := range sensitiveTokens {
		if strings.Contains(normalized, token) {
			return true
		}
	}

	return false
}

func redactURLString(value string) string {
	if !strings.Contains(value, "://") {
		return value
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}

	if parsed.User != nil {
		username := parsed.User.Username()
		if username == "" {
			username = "redacted"
		}
		parsed.User = url.UserPassword(username, "[REDACTED]")
	}

	query := parsed.Query()
	for key := range query {
		if isSensitiveKey(key) {
			query.Set(key, "[REDACTED]")
		}
	}
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

func strconvQuote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "\"\""
	}
	return string(encoded)
}

// credentialField matches a JSON field whose name marks it sensitive, and its
// string value.
//
// Matched in the text rather than decoded and encoded again. That covers a
// body that is not JSON as a whole -- a page with a JSON blob inside it -- and
// leaves one that is as it was: re-encoding sorted its keys, escaped its angle
// brackets and rounded integers past 2^53.
var credentialField = regexp.MustCompile(`(?i)("[a-z0-9_.-]*(?:token|password|passwd|passphrase|secret|authorization|cookie|apikey|api-key|api_key|private[_-]?key|credential)[a-z0-9_.-]*"\s*:\s*")((?:[^"\\]|\\.)*)(")`)

// credentialInURL matches a URL carrying userinfo, which is where a token
// hides in text that is not a URL field: clone links, Location headers, and the
// echoed request line in an upstream error page. The slashes may arrive escaped,
// as some JSON encoders write them, and the secret may contain a slash, as a
// base64 token can.
var credentialInURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*:(?:\\?/){2})([^/\\\s:@"']+):([^\s@"']+)@`)

// credentialQuery matches a credential carried as a query parameter.
var credentialQuery = regexp.MustCompile(`(?i)([?&;](?:access_token|private_token|token|api[_-]?key|password|passwd|secret|client_secret|auth|sig|signature)=)([^&\s"'<#]+)`)

// cookieHeader matches a Cookie or Set-Cookie header. The value is a list of
// pairs, any of which can be a session, so it runs to the end of the line
// rather than to the first semicolon.
var cookieHeader = regexp.MustCompile(`(?i)((?:set-)?cookie["']?\s*[:=]\s*["']?)([^\r\n"'<]+)`)

// credentialHeader matches a header, or a key shaped like one, whose value is a
// credential -- through to the credential itself.
//
// It has to reach past a scheme and past quoting. Written as \S+ it matched
// "Bearer" and left the token after it. Written to stop at a quote, it matched
// nothing of `Authorization: "Bearer X"` and put the marker in front of the
// secret, which looks redacted and is not. So quotes and HTML spacing between
// the name and the value are skipped, a scheme word is kept for the reader,
// and the value runs to whatever ends it.
// The separator also skips an escaped quote, and the value stops before a
// backslash. A body that arrives as JSON writes the header as
// `Authorization: \"Bearer X\"`, and a pattern that could not skip the `\"`
// matched the backslash alone: it replaced the escape, left the token, and
// broke the JSON for whoever parsed it next.
var credentialHeader = regexp.MustCompile(`(?i)((?:proxy-)?authorization|x-[a-z0-9-]*(?:token|key|secret)|api[-_]?key)((?:\\?["'])?\s*[:=](?:\s|&nbsp;|\\?["'])*)((?:bearer|basic|token|negotiate|digest)(?:\s|&nbsp;)+)?([^\s"'<>;,}&\\]+)`)

// credentialScheme matches a credential that arrives with its scheme and no
// header name in front of it: "rejected Bearer abc123..." in a sentence a
// server wrote. Length is the only thing separating that from prose, so the
// replacement checks it rather than the pattern.
var credentialScheme = regexp.MustCompile(`(?i)\b(bearer|basic)(\s+)([A-Za-z0-9._~+/=-]+)`)

// schemeCredentialLength is how long a word after Bearer or Basic has to be
// before it is treated as a credential. "authentication" is fourteen, and a
// token is longer than any word a message would put there.
const schemeCredentialLength = 16

// credentialAssignment matches key=value where no query string introduced it:
// a form-encoded body, or a fragment of one echoed into an error.
var credentialAssignment = regexp.MustCompile(`(?i)(^|[\s,{(])((?:access_token|private_token|token|api[_-]?key|password|passwd|secret|client_secret)=)([^&\s"'<#]+)`)

// credentialQuotedField is credentialField for a document that quotes its keys
// with apostrophes, which a Python or Ruby server prints when it repr()s a
// dictionary into an error.
var credentialQuotedField = regexp.MustCompile(`(?i)('[a-z0-9_.-]*(?:token|password|passwd|secret|credential|passphrase|private[_-]?key)[a-z0-9_.-]*'\s*:\s*')([^']*)(')`)

// credentialElement matches an XML element whose name marks it sensitive. The
// SOAP and LDAP faces of an enterprise stack answer in XML, and a proxy in
// front of Bitbucket can be the thing that fails.
var credentialElement = regexp.MustCompile(`(?i)(<([a-z0-9:_.-]*(?:token|password|passwd|secret|credential|passphrase|private[_-]?key)[a-z0-9:_.-]*)[^>]*>)([^<]+)(</)`)

// RedactText removes credentials from free text.
//
// The log fields have RedactFields, which works because a field has a name.
// Free text has none: an upstream error body is whatever the server chose to
// send, and it reaches the user through error.message. A server that echoes the
// request line, a clone URL, or an Authorization header puts a live credential
// in there, and nothing on that path was redacting anything (#574).
//
// Every pass is a match on the text, for the shapes a credential arrives in: a
// sensitive JSON field, a URL with userinfo, a query parameter, a cookie, and
// a credential header. Matching is a blunt instrument and will not catch a
// secret a server invents a new shape for -- a bare token as a URL's username
// is indistinguishable from a username -- so this is the floor, not the
// ceiling.
func RedactText(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}

	text = credentialField.ReplaceAllString(text, "${1}[REDACTED]${3}")
	text = credentialQuotedField.ReplaceAllString(text, "${1}[REDACTED]${3}")
	text = credentialElement.ReplaceAllString(text, "${1}[REDACTED]${4}")
	text = credentialInURL.ReplaceAllString(text, "${1}${2}:[REDACTED]@")
	text = credentialQuery.ReplaceAllString(text, "${1}[REDACTED]")
	text = credentialAssignment.ReplaceAllString(text, "${1}${2}[REDACTED]")
	text = cookieHeader.ReplaceAllString(text, "${1}[REDACTED]")
	text = credentialHeader.ReplaceAllString(text, "${1}${2}${3}[REDACTED]")

	return redactSchemeCredentials(text)
}

// redactSchemeCredentials removes a credential that arrives with its scheme and
// nothing else in front of it.
//
// Length decides, because the pattern alone cannot: "Bearer authentication is
// required" is prose, and the word after the scheme is the only difference.
// Anything at least as long as schemeCredentialLength is treated as a secret,
// which errs towards redacting a long word rather than printing a token.
func redactSchemeCredentials(text string) string {
	return credentialScheme.ReplaceAllStringFunc(text, func(match string) string {
		groups := credentialScheme.FindStringSubmatch(match)
		if len(groups) != 4 || len(groups[3]) < schemeCredentialLength {
			return match
		}

		return groups[1] + groups[2] + "[REDACTED]"
	})
}
