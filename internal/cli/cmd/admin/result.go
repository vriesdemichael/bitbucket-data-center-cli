package admincmd

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

// Health is what `bb admin health` reports.
//
// Declared here rather than pointing the schema at httpclient.HealthStatus,
// which is the transport's own type: publishing that would make a change to an
// internal struct a change to bb's contract without anyone deciding it should
// be, which is the coupling this modelling exists to break.
type Health struct {
	Healthy       bool   `json:"healthy" jsonschema:"Whether the instance answered at all."`
	StatusCode    int    `json:"status_code" jsonschema:"HTTP status the health probe received."`
	Authenticated bool   `json:"authenticated" jsonschema:"Whether the configured credential was accepted. False here with healthy true means the instance is reachable but the credential is not working."`
	Message       string `json:"message,omitempty" jsonschema:"Detail from the instance, when it gave one."`
}

func init() {
	result.Declare("admin health", result.For[Health](nil))
}

// healthFrom converts the transport's report into the reported shape.
func healthFrom(status httpclient.HealthStatus) Health {
	return Health{
		Healthy:       status.Healthy,
		StatusCode:    status.StatusCode,
		Authenticated: status.Authenticated,
		Message:       status.Message,
	}
}
