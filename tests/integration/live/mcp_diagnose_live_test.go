//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// diagnoseOutput is the part of diagnose's answer these tests read.
type diagnoseOutput struct {
	Build struct {
		Version string `json:"version"`
	} `json:"build"`
	Configuration *struct {
		Settings []struct {
			Name       string `json:"name"`
			Configured bool   `json:"configured"`
			Source     string `json:"source"`
		} `json:"settings"`
	} `json:"configuration"`
	Connection struct {
		Scheme           string `json:"scheme"`
		Reachable        bool   `json:"reachable"`
		BitbucketVersion string `json:"bitbucket_version"`
		Credential       string `json:"credential"`
		Problem          string `json:"problem"`
	} `json:"connection"`
	Client struct {
		Name          string `json:"name"`
		DisplaysViews bool   `json:"displays_views"`
	} `json:"client"`
	Pages struct {
		DiagnosePageReads int `json:"diagnose_page_reads"`
		CheckReads        int `json:"check_reads"`
		CheckCalls        int `json:"check_calls"`
	} `json:"pages"`
	Findings []string `json:"findings"`
}

// liveViewClient is a client that renders views, as a host declares it.
func liveViewClient() *mcp.ClientOptions {
	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
	return &mcp.ClientOptions{Capabilities: capabilities}
}

func callLiveDiagnose(t *testing.T, session *mcp.ClientSession) (diagnoseOutput, string) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "diagnose"})
	if err != nil || result.IsError {
		t.Fatalf("diagnose: %v %s", err, mcpResultText(result))
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out diagnoseOutput
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("decode diagnose: %v\n%s", err, encoded)
	}
	return out, mcpResultText(result) + "\n" + string(encoded)
}

// TestLiveMCPDiagnoseGoesTheWayAToolGoes: diagnose reports from inside the server,
// as the client started it: the real server's version, that the credential works,
// and where its settings came from, with nothing that names the instance or the
// person, since what it answers gets pasted into issues.
func TestLiveMCPDiagnoseGoesTheWayAToolGoes(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	properties, err := harness.liveJSON(context.Background(), "GET", "/rest/api/latest/application-properties", nil)
	if err != nil {
		t.Fatalf("read the instance's version: %v", err)
	}
	version := asString(properties["version"])
	if version == "" {
		t.Fatalf("the instance says no version: %v", properties)
	}
	base, err := url.Parse(harness.config.BitbucketURL)
	if err != nil {
		t.Fatal(err)
	}

	executeLiveMCPServerAs(t, liveViewClient(), func(session *mcp.ClientSession) {
		out, answer := callLiveDiagnose(t, session)

		if !out.Connection.Reachable || out.Connection.Credential != "works" || out.Connection.Problem != "" {
			t.Errorf("connection = %+v, want reached with a credential that works", out.Connection)
		}
		if out.Connection.BitbucketVersion != version {
			t.Errorf("Bitbucket's version = %q, want the instance's %q", out.Connection.BitbucketVersion, version)
		}
		if out.Connection.Scheme != base.Scheme {
			t.Errorf("scheme = %q, want %q", out.Connection.Scheme, base.Scheme)
		}
		if out.Client.Name != "bb-live-test" || !out.Client.DisplaysViews {
			t.Errorf("client = %+v", out.Client)
		}
		if out.Configuration == nil || len(out.Configuration.Settings) == 0 {
			t.Errorf("configuration = %+v, want each setting and where it came from", out.Configuration)
		}
		for _, name := range []string{base.Host, base.Hostname(), harness.config.BitbucketPassword, harness.config.BitbucketToken} {
			if name != "" && strings.Contains(answer, name) {
				t.Errorf("diagnose's answer names %q:\n%s", name, answer)
			}
		}
		if len(out.Findings) != 0 {
			t.Errorf("a sound setup has findings: %v", out.Findings)
		}

		// A host reads the page and passes the check's calls: the next diagnose
		// counts what reached the server, and the page declares this instance.
		read, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "ui://bb/diagnose"})
		if err != nil {
			t.Fatalf("read the page: %v", err)
		}
		declared, err := json.Marshal(read.Contents[0].Meta)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(declared), base.Scheme+"://"+base.Host) {
			t.Errorf("the page declares %s, want this instance", declared)
		}
		if _, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "ui://bb/diagnose/fixture.png"}); err != nil {
			t.Fatalf("read a fixture: %v", err)
		}
		if result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "diagnose_check", Arguments: map[string]any{"report": map[string]any{"host": harness.config.BitbucketURL}}}); err != nil || result.IsError {
			t.Fatalf("diagnose_check: %v %s", err, mcpResultText(result))
		}
		again, answer := callLiveDiagnose(t, session)
		if again.Pages.DiagnosePageReads != 1 || again.Pages.CheckReads != 1 || again.Pages.CheckCalls != 1 {
			t.Errorf("pages = %+v, want the page read, a fixture read and a call", again.Pages)
		}
		if strings.Contains(answer, base.Host) {
			t.Errorf("the page's report, read back, names the instance:\n%s", answer)
		}
		if !strings.Contains(answer, "\\u003cbitbucket\\u003e") && !strings.Contains(answer, "<bitbucket>") {
			t.Errorf("the page's report did not come back:\n%s", answer)
		}
	}, "ai", "mcp", "serve")
}

// TestLiveMCPDiagnoseSaysACredentialWasRefused: a wrong password against a server
// that answers is a refusal, not a server out of reach, and Bitbucket answers one
// on every request, even those that need no credential.
func TestLiveMCPDiagnoseSaysACredentialWasRefused(t *testing.T) {
	harness := newLiveHarness(t)
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", harness.username())
	t.Setenv("BITBUCKET_PASSWORD", "definitely-not-the-password")
	t.Setenv("ADMIN_PASSWORD", "definitely-not-the-password")

	executeLiveMCPServerAs(t, liveViewClient(), func(session *mcp.ClientSession) {
		out, answer := callLiveDiagnose(t, session)
		if !out.Connection.Reachable || out.Connection.Credential != "refused" {
			t.Errorf("connection = %+v, want reached, and the credential refused", out.Connection)
		}
		if !strings.Contains(out.Connection.Problem, "refused the credential") {
			t.Errorf("the problem reads %q", out.Connection.Problem)
		}
		if len(out.Findings) == 0 || !strings.Contains(strings.Join(out.Findings, "\n"), "refused the credential") {
			t.Errorf("the findings read %v, want what to fix", out.Findings)
		}
		if strings.Contains(answer, "definitely-not-the-password") {
			t.Errorf("diagnose's answer holds the password:\n%s", answer)
		}
	}, "ai", "mcp", "serve")
}
