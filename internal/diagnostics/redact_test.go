package diagnostics

import (
	"strings"
	"testing"
)

// TestRedactTextCatchesTheShapesACredentialArrivesIn is #574's security half.
//
// The first version caught three shapes. A review sent fourteen realistic
// bodies through it and every one of them kept its secret -- one of them
// behind a marker, `Authorization:[REDACTED]"Bearer X"`, which looks redacted
// and is not.
func TestRedactTextCatchesTheShapesACredentialArrivesIn(t *testing.T) {
	t.Parallel()

	const secret = "S3cr3tT0k3nValue"

	for name, text := range map[string]string{
		"a clone URL":                             "could not reach https://x-token-auth:" + secret + "@bitbucket.example/scm/p/r.git",
		"a token with a slash in a clone URL":     "could not reach https://svc:ab/cd" + secret + "@bitbucket.example/scm/p/r.git",
		"a clone URL with escaped slashes":        `{"href":"https:\/\/svc:` + secret + `@bitbucket.example\/scm"}`,
		"an Authorization header":                 "rejected Authorization: Bearer " + secret,
		"a quoted Authorization value":            `rejected Authorization: "Bearer ` + secret + `"`,
		"an HTML-spaced Authorization value":      "<td>Authorization:&nbsp;Basic " + secret + "</td>",
		"a header map written with single quotes": `headers {'Authorization': 'Bearer ` + secret + `'}`,
		"a JSON Authorization field in text":      `request was {"Authorization":"Bearer ` + secret + `"} and failed`,
		"a query token":                           "GET /rest/api?access_token=" + secret + "&limit=1",
		"a private_token":                         "https://bitbucket.example/p?private_token=" + secret,
		"a cookie":                                "Cookie: theme=dark; JSESSIONID=" + secret,
		"a set-cookie":                            "Set-Cookie: JSESSIONID=" + secret + "; Path=/",
		"an X-Auth-Token header":                  "X-Auth-Token: " + secret,
		"a password in JSON that does not parse":  `{"password":"` + secret + `",`,
		"a secret in JSON inside a page":          `<script>var config = {"apiToken":"` + secret + `"};</script>`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			redacted := RedactText(text)
			if strings.Contains(redacted, secret) {
				t.Fatalf("the secret survived:\n%s", redacted)
			}
			if !strings.Contains(redacted, "[REDACTED]") {
				t.Fatalf("the secret was removed without a marker, so a reader cannot tell anything was:\n%s", redacted)
			}
		})
	}
}

// A body that is JSON keeps its shape. Re-encoding it sorted the keys, escaped
// its angle brackets and rounded integers past 2^53.
func TestRedactTextLeavesTheRestOfADocumentAlone(t *testing.T) {
	t.Parallel()

	body := `{"zeta":1,"alpha":12345678901234567890,"note":"<b>bold</b>","token":"S3cr3t"}`
	redacted := RedactText(body)

	want := `{"zeta":1,"alpha":12345678901234567890,"note":"<b>bold</b>","token":"[REDACTED]"}`
	if redacted != want {
		t.Fatalf("got  %s\nwant %s", redacted, want)
	}

	// And ordinary text is not a credential.
	for _, text := range []string{
		"Authors may not update their status.",
		"Repository https://bitbucket.example/projects/P/repos/r does not exist.",
		"the token list is empty",
	} {
		if got := RedactText(text); got != text {
			t.Errorf("ordinary text was changed:\n%s\n%s", text, got)
		}
	}
}
