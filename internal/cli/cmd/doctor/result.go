package doctorcmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// Report is what `bb doctor` returns.
//
// ok is the verdict. Under --json the exit status is always zero -- machine
// output is a single document, and a failing exit would replace the report with
// an error envelope -- so this field is how a caller learns the answer.
type Report struct {
	OK       bool      `json:"ok" jsonschema:"Whether every configuration file bb reads is valid. Without --json, an invalid file exits 1."`
	Files    []File    `json:"files" jsonschema:"The stored, workspace and system configuration files, in that order."`
	Settings []Setting `json:"settings" jsonschema:"Every effective setting and where it came from, resolved from each file that parses, including one the schema rejects."`
	Keyring  Keyring   `json:"keyring" jsonschema:"The OS keyring, checked only when keyring-backed storage is required."`
}

// File is one configuration file and what is wrong with it.
type File struct {
	Tier          string       `json:"tier" jsonschema:"Which configuration file this is."`
	Path          string       `json:"path,omitempty" jsonschema:"Where the file is. Absent when no workspace file was found, or the stored file's location could not be worked out."`
	PathFrom      string       `json:"pathFrom" jsonschema:"What chose the path: the variable that set it, default, search for a workspace file found above the working directory, or machine for the fixed system location."`
	Read          bool         `json:"read" jsonschema:"Whether bb reads this file at all."`
	NotRead       string       `json:"notRead,omitempty" jsonschema:"Why bb does not read it, when it does not."`
	Exists        bool         `json:"exists" jsonschema:"Whether the file is there."`
	Parses        bool         `json:"parses" jsonschema:"Whether it is YAML bb can decode. False for a file that is not there."`
	MatchesSchema bool         `json:"matchesSchema" jsonschema:"Whether it has no violations. False for a file that is not there or does not parse."`
	Valid         bool         `json:"valid" jsonschema:"Whether bb would load it. A file bb does not read, or that is not there, is valid."`
	Problem       string       `json:"problem,omitempty" jsonschema:"What stopped the file being checked further: it could not be read, or it does not parse."`
	Violations    []Violation  `json:"violations" jsonschema:"Every key the configuration schema rejects, not only the first."`
	Ignored       []IgnoredKey `json:"ignored" jsonschema:"Keys the schema accepts that bb does not read from this file."`
	Secrets       []Secret     `json:"secrets" jsonschema:"Hosts with a plaintext credential in this file's insecure_secrets. Never the credential itself."`
}

// Violation is one key the configuration schema rejects.
type Violation struct {
	Key     string `json:"key" jsonschema:"Dotted path to the key at fault. Empty for the file as a whole."`
	Line    int    `json:"line,omitempty" jsonschema:"Line the key is on. For a missing key, the line of the key that should hold it."`
	Problem string `json:"problem" jsonschema:"What is wrong: an unknown key, a missing one, or a value of the wrong type."`
}

// IgnoredKey is a key that means something in another configuration file.
type IgnoredKey struct {
	Key      string   `json:"key" jsonschema:"The top-level key."`
	ReadFrom []string `json:"readFrom" jsonschema:"The configuration files bb does read it from."`
}

// Secret says which plaintext credentials a file holds for a host.
type Secret struct {
	Host     string `json:"host" jsonschema:"The host the credential is for."`
	Token    bool   `json:"token" jsonschema:"Whether a plaintext token is held."`
	Password bool   `json:"password" jsonschema:"Whether a plaintext password is held."`
}

// Setting is one effective setting.
type Setting struct {
	Name       string   `json:"name" jsonschema:"The setting, named as its configuration key where it has one."`
	Value      string   `json:"value,omitempty" jsonschema:"The effective value. Never present for a secret."`
	Secret     bool     `json:"secret" jsonschema:"Whether this is a token or password, whose value is never reported."`
	Configured bool     `json:"configured" jsonschema:"Whether anything sets it. False means the built-in default applies."`
	Source     Source   `json:"source" jsonschema:"Where the effective value came from."`
	Shadowed   []Source `json:"shadowed" jsonschema:"Other places that set it and lost to source."`
	Problem    string   `json:"problem,omitempty" jsonschema:"What a command loading this setting would refuse."`
}

// Source is where a setting came from.
type Source struct {
	Kind string `json:"kind" jsonschema:"What kind of place: a flag, the environment, a .env file, a value passed by the program running bb, one of the three configuration files, the Windows registry, the OS keyring, or the built-in default."`
	Name string `json:"name,omitempty" jsonschema:"The flag, variable, key, keyring entry or registry value."`
	Path string `json:"path,omitempty" jsonschema:"The file, .env file or registry key it was found in."`
}

// Keyring reports on the OS keyring.
type Keyring struct {
	Required   bool   `json:"required" jsonschema:"Whether keyring-backed storage is required."`
	RequiredBy Source `json:"requiredBy,omitzero" jsonschema:"What requires it."`
	Checked    bool   `json:"checked" jsonschema:"Whether the keyring was probed. Only when it is required."`
	Reachable  bool   `json:"reachable" jsonschema:"Whether the probe reached it."`
	Problem    string `json:"problem,omitempty" jsonschema:"Why it could not be reached."`
}

func init() {
	kinds := config.SourceKinds()
	result.Declare("doctor", result.For[Report](map[string][]string{
		"files.tier":              {config.TierStored, config.TierWorkspace, config.TierSystem},
		"settings.source.kind":    kinds,
		"settings.shadowed.kind":  kinds,
		"keyring.requiredBy.kind": kinds,
	}))
}

func reportFrom(diagnosis config.Diagnosis) Report {
	report := Report{
		OK:       true,
		Files:    make([]File, 0, len(diagnosis.Files)),
		Settings: make([]Setting, 0, len(diagnosis.Settings)),
		Keyring: Keyring{
			Required:   diagnosis.Keyring.Required,
			RequiredBy: sourceFrom(diagnosis.Keyring.RequiredBy),
			Checked:    diagnosis.Keyring.Checked,
			Reachable:  diagnosis.Keyring.Reachable,
			Problem:    diagnosis.Keyring.Problem,
		},
	}

	for _, file := range diagnosis.Files {
		report.OK = report.OK && file.Valid()
		report.Files = append(report.Files, fileFrom(file))
	}
	for _, setting := range diagnosis.Settings {
		report.Settings = append(report.Settings, settingFrom(setting))
	}

	return report
}

func fileFrom(file config.DiagnosedFile) File {
	published := File{
		Tier:          file.Tier,
		Path:          file.Path,
		PathFrom:      file.PathFrom,
		Read:          file.Read,
		NotRead:       file.NotRead,
		Exists:        file.Exists,
		Parses:        file.Parses,
		MatchesSchema: file.MatchesSchema,
		Valid:         file.Valid(),
		Problem:       file.Problem,
		Violations:    make([]Violation, 0, len(file.Violations)),
		Ignored:       make([]IgnoredKey, 0, len(file.Ignored)),
		Secrets:       make([]Secret, 0, len(file.Secrets)),
	}
	for _, violation := range file.Violations {
		published.Violations = append(published.Violations, Violation{Key: violation.Key, Line: violation.Line, Problem: violation.Problem})
	}
	for _, ignored := range file.Ignored {
		published.Ignored = append(published.Ignored, IgnoredKey{Key: ignored.Key, ReadFrom: append([]string{}, ignored.ReadFrom...)})
	}
	for _, secret := range file.Secrets {
		published.Secrets = append(published.Secrets, Secret{Host: secret.Host, Token: secret.Token, Password: secret.Password})
	}

	return published
}

func settingFrom(setting config.DiagnosedSetting) Setting {
	published := Setting{
		Name:       setting.Name,
		Value:      setting.Value,
		Secret:     setting.Secret,
		Configured: setting.Configured,
		Source:     sourceFrom(setting.Source),
		Shadowed:   make([]Source, 0, len(setting.Shadowed)),
		Problem:    setting.Problem,
	}
	if published.Secret {
		published.Value = ""
	}
	for _, shadowed := range setting.Shadowed {
		published.Shadowed = append(published.Shadowed, sourceFrom(shadowed))
	}

	return published
}

func sourceFrom(source config.SettingSource) Source {
	return Source{Kind: source.Kind, Name: source.Name, Path: source.Path}
}
