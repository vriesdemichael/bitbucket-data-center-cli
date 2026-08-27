//go:build windows

package config

import (
	"errors"
	"testing"
)

type mockRegistryReader struct {
	integers map[string]uint64
	strings  map[string]string
	strLists map[string][]string
}

func (m *mockRegistryReader) GetIntegerValue(name string) (uint64, uint32, error) {
	if val, ok := m.integers[name]; ok {
		return val, 0, nil
	}
	return 0, 0, errors.New("value not found")
}

func (m *mockRegistryReader) GetStringValue(name string) (string, uint32, error) {
	if val, ok := m.strings[name]; ok {
		return val, 0, nil
	}
	return "", 0, errors.New("value not found")
}

func (m *mockRegistryReader) GetStringsValue(name string) ([]string, uint32, error) {
	if val, ok := m.strLists[name]; ok {
		return val, 0, nil
	}
	return nil, 0, errors.New("value not found")
}

func TestParseRegistryPolicy(t *testing.T) {
	readerInt := &mockRegistryReader{
		integers: map[string]uint64{
			"RequireKeyring":          1,
			"AllowInsecureSkipVerify": 0,
			"DisableUpdate":           1,
		},
		strings: map[string]string{
			"CAFile":        `C:\ProgramData\bb\ca.crt`,
			"UpdateBaseURL": "https://releases.internal/bb",
		},
		strLists: map[string][]string{
			"AllowedHosts": {"https://bitbucket1.internal", "https://bitbucket2.internal"},
		},
	}

	p1 := parseRegistryPolicy(readerInt)
	if p1.RequireKeyring == nil || !*p1.RequireKeyring {
		t.Errorf("expected RequireKeyring=true, got %v", p1.RequireKeyring)
	}
	if p1.AllowInsecureSkipVerify == nil || *p1.AllowInsecureSkipVerify {
		t.Errorf("expected AllowInsecureSkipVerify=false, got %v", p1.AllowInsecureSkipVerify)
	}
	if p1.DisableUpdate == nil || !*p1.DisableUpdate {
		t.Errorf("expected DisableUpdate=true, got %v", p1.DisableUpdate)
	}
	if p1.CAFile != `C:\ProgramData\bb\ca.crt` {
		t.Errorf("expected CAFile, got %s", p1.CAFile)
	}
	if p1.UpdateBaseURL != "https://releases.internal/bb" {
		t.Errorf("expected UpdateBaseURL, got %s", p1.UpdateBaseURL)
	}
	if len(p1.AllowedHosts) != 2 {
		t.Errorf("expected 2 AllowedHosts, got %v", p1.AllowedHosts)
	}

	readerStr := &mockRegistryReader{
		integers: map[string]uint64{},
		strings: map[string]string{
			"RequireKeyring":          "true",
			"AllowInsecureSkipVerify": "false",
			"DisableUpdate":           "1",
			"AllowedHosts":            "https://hostA.internal, https://hostB.internal",
		},
	}

	p2 := parseRegistryPolicy(readerStr)
	if p2.RequireKeyring == nil || !*p2.RequireKeyring {
		t.Errorf("expected RequireKeyring=true from string, got %v", p2.RequireKeyring)
	}
	if p2.AllowInsecureSkipVerify == nil || *p2.AllowInsecureSkipVerify {
		t.Errorf("expected AllowInsecureSkipVerify=false from string, got %v", p2.AllowInsecureSkipVerify)
	}
	if p2.DisableUpdate == nil || !*p2.DisableUpdate {
		t.Errorf("expected DisableUpdate=true from string, got %v", p2.DisableUpdate)
	}
	if len(p2.AllowedHosts) != 2 || p2.AllowedHosts[0] != "https://hostA.internal" || p2.AllowedHosts[1] != "https://hostB.internal" {
		t.Errorf("expected parsed comma-separated hosts, got %v", p2.AllowedHosts)
	}
}
