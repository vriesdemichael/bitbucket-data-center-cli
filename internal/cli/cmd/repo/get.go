package repocmd

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	repositoryservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/repository"
)

// newRepoGetCommand is `bb repo get`, registered with `view` as its gh
// spelling.
//
// A Cobra alias rather than a second registration: gh repo view takes nothing
// bb repo get does not, so there is no flag state for two registrations to
// keep apart, and one command cannot drift from itself (ADR-050).
func newRepoGetCommand(deps Dependencies) *cobra.Command {
	var repositorySelector string
	var includeReadme bool

	cmd := &cobra.Command{
		Use:     "get",
		Aliases: []string{"view"},
		Short:   "Show a repository's details and its README",
		Long: "Show a repository's description, state and clone URLs, followed by its README.\n\n" +
			"The README is the file Bitbucket picks from the default branch, printed as raw markup: bb does not " +
			"render it. A repository without a README says so rather than failing. --readme=false leaves the " +
			"README out and skips the request for it.\n\n" +
			"bb repo view is the gh spelling of the same command. To open the repository in a browser, use bb browse.",
		Example: "  # Describe a repository and print its README\n" +
			"  bb repo get --repo PROJ/repo\n\n" +
			"  # Only the details\n" +
			"  bb repo get --repo PROJ/repo --readme=false",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoRef, err := resolveRepoReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := repositoryservice.NewAdminService(client)
			upstream, err := service.Get(cmd.Context(), repoRef)
			if err != nil {
				return err
			}

			var readme []byte
			found := false
			if includeReadme {
				readme, found, err = service.Readme(cmd.Context(), repoRef)
				if err != nil {
					return err
				}
			}

			view := RepositoryView{
				Repository: result.RepositoryDetailFrom(upstream),
				CloneURLs:  cloneURLsFrom(upstream.Links),
				Readme:     readmeFrom(readme, found),
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), view)
			}

			writeRepositoryView(cmd.OutOrStdout(), view, readme, found, includeReadme)

			return nil
		},
	}

	cmd.Flags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")
	cmd.Flags().BoolVar(&includeReadme, "readme", true, "Include the README; --readme=false leaves it out")

	return cmd
}

// cloneURLsFrom reads the clone links, which the specification types as an
// open map: links.clone is a list of {href, name}.
func cloneURLsFrom(links *map[string]interface{}) []CloneURL {
	urls := []CloneURL{}
	if links == nil {
		return urls
	}

	entries, _ := (*links)["clone"].([]any)
	for _, entry := range entries {
		link, _ := entry.(map[string]any)
		href, _ := link["href"].(string)
		if strings.TrimSpace(href) == "" {
			continue
		}
		name, _ := link["name"].(string)
		urls = append(urls, CloneURL{Name: name, URL: href})
	}

	return urls
}

// readmeFrom carries the README as text when it is valid UTF-8 and as base64
// when it is not, for the reason RawFile does: a JSON string cannot hold
// arbitrary bytes, and Go's encoder would replace them without saying so.
func readmeFrom(content []byte, found bool) *Readme {
	if !found {
		return nil
	}
	if utf8.Valid(content) {
		return &Readme{Encoding: "utf-8", Content: string(content)}
	}

	return &Readme{Encoding: "base64", Content: base64.StdEncoding.EncodeToString(content)}
}

// writeRepositoryView prints the details, then the README exactly as it is
// stored.
func writeRepositoryView(writer io.Writer, view RepositoryView, readme []byte, found, requested bool) {
	detail := view.Repository
	line := func(label, value string) {
		fmt.Fprintf(writer, "%s %s\n", style.Label.Render(label+":"), value)
	}
	yesNo := func(value bool) string {
		if value {
			return "yes"
		}
		return "no"
	}

	fmt.Fprintln(writer, style.Resource.Render(detail.ProjectKey+"/"+detail.Slug))
	line("Name", detail.Name)
	if detail.Description != "" {
		line("Description", detail.Description)
	}
	line("State", detail.State)
	if detail.DefaultBranch != "" {
		line("Default branch", detail.DefaultBranch)
	}
	line("Public", yesNo(detail.Public))
	line("Forkable", yesNo(detail.Forkable))
	line("Archived", yesNo(detail.Archived))
	if detail.Origin != nil {
		line("Forked from", detail.Origin.ProjectKey+"/"+detail.Origin.Slug)
	}
	for _, clone := range view.CloneURLs {
		line("Clone ("+clone.Name+")", clone.URL)
	}

	if !requested {
		return
	}

	fmt.Fprintln(writer)
	if !found {
		fmt.Fprintln(writer, style.Empty.Render("No README"))
		return
	}

	_, _ = writer.Write(readme)
	if len(readme) > 0 && !bytes.HasSuffix(readme, []byte("\n")) {
		fmt.Fprintln(writer)
	}
}
